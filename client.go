package main

import (
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"time"
)

const (
	defaultTimeout = 3 * time.Second
)

func buildUnixSocketClient(socketAddr string, timeout time.Duration) (*http.Client, error) {
	transport := &http.Transport{
		DisableKeepAlives: true,
		Dial: func(proto, addr string) (conn net.Conn, err error) {
			return net.Dial("unix", "\x00"+socketAddr)
		},
	}

	client := &http.Client{
		Transport: transport,
	}

	if timeout > 0 {
		client.Timeout = timeout
	}

	return client, nil
}

func doGet(socketAddr string, timeoutInSeconds time.Duration, urlPath string) ([]byte, error) {
	client, err := buildUnixSocketClient(socketAddr, timeoutInSeconds)
	if err != nil {
		return nil, err
	}

	resp, err := client.Get(fmt.Sprintf("http://shim%s", urlPath))
	if err != nil {
		return nil, err
	}

	defer func() {
		resp.Body.Close()
	}()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return body, nil
}
