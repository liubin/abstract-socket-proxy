package main

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

var data = `
cat /proc/net/unix | grep shim
ffffa0b075262000: 00000002 00000000 00010000 0001 01 25032547 @/containerd-shim/k8s.io/fb76908c5f81e5820226357191af190b8623fa914932926196d80a8a5587161b/shim.sock@
ffffa0b1997e8800: 00000002 00000000 00010000 0001 01 25034882 @/containerd-shim/k8s.io/fb76908c5f81e5820226357191af190b8623fa914932926196d80a8a5587161b/shim-monitor.sock@
ffffa0b19aa77800: 00000003 00000000 00000000 0001 03 25034159 @/containerd-shim/k8s.io/fb76908c5f81e5820226357191af190b8623fa914932926196d80a8a5587161b/shim.sock@
ffffa0b17ab70000: 00000003 00000000 00000000 0001 03 25034860 @/containerd-shim/k8s.io/fb76908c5f81e5820226357191af190b8623fa914932926196d80a8a5587161b/shim.sock@
`

func TestParseSocketAddrs(t *testing.T) {
	assert := assert.New(t)
	matchPattern = regexp.MustCompile(`@/containerd-shim/k8s.io/(?P<sandbox>[0-9a-zA-Z]*)/shim-monitor.sock@`)

	result, err := parseSocketAddrs(data)
	assert.Nil(err, "err should be nil")
	assert.Equal(1, len(result))
	for addrFile, socketAddr := range result {
		assert.Equal("/containerd-shim/k8s.io/fb76908c5f81e5820226357191af190b8623fa914932926196d80a8a5587161b/shim-monitor.sock", addrFile, "addr is not expected")

		assert.Equal(map[string]string{
			"sandbox": "fb76908c5f81e5820226357191af190b8623fa914932926196d80a8a5587161b",
		}, socketAddr.tags, "tags is not expected")
	}
}
