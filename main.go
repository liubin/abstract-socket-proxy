package main

import (
	"flag"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"text/template"
	"time"

	"github.com/sirupsen/logrus"
)

var monitorListenAddr = flag.String("listen-address", ":8090", "The address to listen on for HTTP requests.")
var logLevel = flag.String("log-level", "info", "Log level of logrus(trace/debug/info/warn/error/fatal/panic).")
var addressPattern = flag.String("pattern", "", "Pattern for matching abstract socket file")

// These values are overridden via ldflags
var (
	appName = "abstract-socket-proxy"
	// version is the kata monitor version.
	version = "0.1.0"

	GitCommit = "unknown-commit"

	matchPattern *regexp.Regexp
)

var logger = logrus.WithFields(logrus.Fields{
	"name": "kata-monitor",
	"pid":  os.Getpid(),
})

type versionInfo struct {
	AppName   string
	Version   string
	GitCommit string
	GoVersion string
	Os        string
	Arch      string
}

var versionTemplate = `{{.AppName}}
 Version:	{{.Version}}
 Go version:	{{.GoVersion}}
 Git commit:	{{.GitCommit}}
 OS/Arch:	{{.Os}}/{{.Arch}}
`

func printVersion(ver versionInfo) {
	t, err := template.New("version").Parse(versionTemplate)

	if err = t.Execute(os.Stdout, ver); err != nil {
		panic(err)
	}
}

func main() {
	ver := versionInfo{
		AppName:   appName,
		Version:   version,
		GoVersion: runtime.Version(),
		Os:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		GitCommit: GitCommit,
	}

	if len(os.Args) == 2 && (os.Args[1] == "--version" || os.Args[1] == "version") {
		printVersion(ver)
		return
	}

	flag.Parse()

	// init logrus
	initLog()

	logrus.WithField("version", ver).Info("starting")

	if *addressPattern == "" {
		panic("addressPattern not set")
	}

	logrus.Infof("addressPattern: %s", *addressPattern)

	matchPattern = regexp.MustCompile(*addressPattern)

	// setup handlers, now only metrics is supported
	m := http.NewServeMux()
	m.Handle("/metrics", http.HandlerFunc(processMetricsRequest))
	// listening on the server
	svr := &http.Server{
		Handler: m,
		Addr:    *monitorListenAddr,
	}
	logrus.Fatal(svr.ListenAndServe())
}

// initLog setup logger
func initLog() {
	// set log level, default to warn
	level, err := logrus.ParseLevel(*logLevel)
	if err != nil {
		level = logrus.WarnLevel
	}
	logger.Logger.SetLevel(level)
	logger.Logger.Formatter = &logrus.TextFormatter{TimestampFormat: time.RFC3339Nano}
}
