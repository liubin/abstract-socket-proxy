package main

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
)

const (
	unixSocketFile        = "/proc/net/unix"
	contentTypeHeader     = "Content-Type"
	contentEncodingHeader = "Content-Encoding"
	promNamespace         = "prom_proxy"
	acceptEncodingHeader  = "Accept-Encoding"
)

var (
	listeningSocketCount = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: promNamespace,
		Name:      "listening_socket_count",
		Help:      "Listening socket count.",
	})

	scrapeCount = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: promNamespace,
		Name:      "scrape_count",
		Help:      "Scape count.",
	})

	scrapeFailedCount = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: promNamespace,
		Name:      "scrape_failed_count",
		Help:      "Failed scape count.",
	})

	scrapeDurationsHistogram = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: promNamespace,
		Name:      "scrape_durations_histogram_milliseconds",
		Help:      "Time used to scrape from shims",
		Buckets:   prometheus.ExponentialBuckets(1, 2, 10),
	})

	gzipPool = sync.Pool{
		New: func() interface{} {
			return gzip.NewWriter(nil)
		},
	}
)

func init() {
	prometheus.MustRegister(listeningSocketCount)
	prometheus.MustRegister(scrapeCount)
	prometheus.MustRegister(scrapeFailedCount)
	prometheus.MustRegister(scrapeDurationsHistogram)
}

func processMetricsRequest(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	scrapeCount.Inc()
	defer func() {
		scrapeDurationsHistogram.Observe(float64(time.Since(start).Nanoseconds() / int64(time.Millisecond)))
	}()

	// prepare writer for writing response.
	contentType := expfmt.Negotiate(r.Header)

	// set response header
	header := w.Header()
	header.Set(contentTypeHeader, string(contentType))

	// create writer
	writer := io.Writer(w)
	if gzipAccepted(r.Header) {
		header.Set(contentEncodingHeader, "gzip")
		gz := gzipPool.Get().(*gzip.Writer)
		defer gzipPool.Put(gz)

		gz.Reset(w)
		defer gz.Close()

		writer = gz
	}

	// create encoder to encode metrics.
	encoder := expfmt.NewEncoder(writer, contentType)

	// gather metrics collected for management agent.
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		logger.WithError(err).Error("failed to Gather metrics from prometheus.DefaultGatherer")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(err.Error()))
		return
	}

	// encode metric gathered in current process
	if err := encodeMetricFamily(mfs, encoder); err != nil {
		logger.WithError(err).Warnf("failed to encode metrics")
	}

	// aggregate socket metrics and write to response by encoder
	if err := aggregateSocketMetrics(encoder); err != nil {
		logger.WithError(err).Errorf("failed aggregateSocketMetrics")
		scrapeFailedCount.Inc()
	}
}

func encodeMetricFamily(mfs []*dto.MetricFamily, encoder expfmt.Encoder) error {
	for i := range mfs {
		metricFamily := mfs[i]

		// if metricFamily.Name != nil && !strings.HasPrefix(*metricFamily.Name, promNamespaceMonitor) {
		// 	metricFamily.Name = string2Pointer(promNamespaceMonitor + "_" + *metricFamily.Name)
		// }

		// encode and write to output
		if err := encoder.Encode(metricFamily); err != nil {
			return err
		}
	}
	return nil
}

type socketAddr struct {
	addr string
	tags map[string]string
}

func readUnixSocketFile() (string, error) {
	bytes, err := ioutil.ReadFile(unixSocketFile)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func getSocketAddrs() (map[string]socketAddr, error) {
	body, err := readUnixSocketFile()
	if err != nil {
		return nil, err
	}
	return parseSocketAddrs(body)
}

func parseSocketAddrs(body string) (map[string]socketAddr, error) {
	if matchPattern == nil {
		return nil, fmt.Errorf("matchPattern not set")
	}

	socketAddrs := map[string]socketAddr{}
	lines := strings.Split(body, "\n")

	for _, line := range lines {
		ss := strings.Split(line, " ")
		if len(ss) != 8 {
			continue
		}

		addr := ss[7]
		match := matchPattern.FindStringSubmatch(addr)
		if len(match) == 0 {
			continue
		}

		// remove @ at head and tail
		addr = strings.ReplaceAll(addr, "@", "")
		if _, found := socketAddrs[addr]; found {
			continue
		}

		tags := map[string]string{}
		for i, name := range matchPattern.SubexpNames() {
			if i != 0 && name != "" {
				tags[name] = match[i]
			}
		}
		socketAddrs[addr] = socketAddr{
			addr: addr,
			tags: tags,
		}
	}

	return socketAddrs, nil
}

func aggregateSocketMetrics(encoder expfmt.Encoder) error {
	socketAddrs, err := getSocketAddrs()
	if err != nil {
		return err
	}

	// save listening socket count as a metrics.
	listeningSocketCount.Set(float64(len(socketAddrs)))

	if len(socketAddrs) == 0 {
		return nil
	}

	// socketMetricsList contains list of MetricFamily list from one socket.
	socketMetricsList := make([][]*dto.MetricFamily, 0)

	wg := &sync.WaitGroup{}
	// used to receive response
	results := make(chan []*dto.MetricFamily, len(socketAddrs))

	// get metrics from one socket's
	for _, sa := range socketAddrs {
		wg.Add(1)
		go func(sa socketAddr, results chan<- []*dto.MetricFamily) {
			socketMetrics, err := getSocketMetrics(sa.addr, sa.tags)
			if err != nil {
				logger.WithError(err).WithField("addr", sa.addr).Errorf("failed to get metrics from one socket")
			}

			results <- socketMetrics
			wg.Done()
			logger.WithField("addr", sa.addr).Debug("job finished")
		}(sa, results)

		logger.WithField("socketAddr", sa).Debug("job started")
	}

	wg.Wait()
	logger.Debug("all job finished")
	close(results)

	// get all job result from chan
	for socketMetrics := range results {
		if socketMetrics != nil {
			socketMetricsList = append(socketMetricsList, socketMetrics)
		}
	}

	if len(socketMetricsList) == 0 {
		return nil
	}

	// metricsMap used to aggregate metrics from multiple sockets
	// key is MetricFamily.Name, and value is list of MetricFamily from multiple sockets
	metricsMap := make(map[string]*dto.MetricFamily)
	// merge MetricFamily list for the same MetricFamily.Name from multiple sockets.
	for i := range socketMetricsList {
		socketMetrics := socketMetricsList[i]
		for j := range socketMetrics {
			mf := socketMetrics[j]
			key := *mf.Name

			// add MetricFamily.Metric to the exists MetricFamily instance
			if oldmf, found := metricsMap[key]; found {
				oldmf.Metric = append(oldmf.Metric, mf.Metric...)
			} else {
				metricsMap[key] = mf
			}
		}
	}

	// write metrics to response.
	for _, mf := range metricsMap {
		if err := encoder.Encode(mf); err != nil {
			return err
		}
	}

	return nil
}

func getSocketMetrics(socketAddr string, tags map[string]string) ([]*dto.MetricFamily, error) {
	body, err := doGet(socketAddr, defaultTimeout, *metricsPath)
	if err != nil {
		return nil, err
	}

	logger.WithField("metricsPath", *metricsPath).Debugf("getSocketMetrics")
	return parsePrometheusMetrics(tags, body)
}

// parsePrometheusMetrics will decode metrics from Prometheus text format
// and return array of *dto.MetricFamily with an ASC order
func parsePrometheusMetrics(tags map[string]string, body []byte) ([]*dto.MetricFamily, error) {
	reader := bytes.NewReader(body)
	decoder := expfmt.NewDecoder(reader, expfmt.FmtText)

	// decode metrics from socket to MetricFamily
	list := make([]*dto.MetricFamily, 0)
	for {
		mf := &dto.MetricFamily{}
		if err := decoder.Decode(mf); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}

		metricList := mf.Metric
		for j := range metricList {
			metric := metricList[j]
			for k, v := range tags {
				metric.Label = append(metric.Label, &dto.LabelPair{
					Name:  string2Pointer(k),
					Value: string2Pointer(v),
				})
			}
		}

		list = append(list, mf)
	}

	// sort ASC
	sort.SliceStable(list, func(i, j int) bool {
		b := strings.Compare(*list[i].Name, *list[j].Name)
		return b < 0
	})

	return list, nil
}

func gzipAccepted(header http.Header) bool {
	a := header.Get(acceptEncodingHeader)
	parts := strings.Split(a, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "gzip" || strings.HasPrefix(part, "gzip;") {
			return true
		}
	}
	return false
}

func string2Pointer(s string) *string {
	return &s
}
