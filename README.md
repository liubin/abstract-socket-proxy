# abstract-socket-proxy

An Prometheus proxy for abstract socket address

Working as a node agent/proxy, scrapes metrics from found Unix socket, and returns to Prometheus.

## How to use

```bash
$ abstract-socket-proxy --pattern "@/containerd-shim/k8s.io/(?P<sandbox>[0-9a-zA-Z]*)/shim-monitor.sock@"
```

This will read `/proc/net/unix` and match lines by the `pattern` option, and get metrics through the socket.

You can specify the metrics path by `-metrics-path` option, the default value is `/metrics`.

The `pattern` option dosn't only used to filter sockets, but also can be used to parse the file path and convert the named pattern to metrics labels. For the upon pattern, the proxy will also add a label `"sandbox": "matched-string"` to metrics scraped from the target socket path.

## Todos

- [ ] support normal Unix domain socket.
