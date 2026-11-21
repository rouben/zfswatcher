package main

import (
	"net"
	"os"
	"regexp"
	"strconv"
	"testing"
)

func unprivilegedPort(t *testing.T, addr string) {
	t.Helper()
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("bad address %q: %v", addr, err)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n <= 1024 {
		t.Errorf("address %q must use a port above 1024", addr)
	}
}

// The service runs without root, so the default must not need port 80.
func TestDefaultWwwBind(t *testing.T) {
	unprivilegedPort(t, defaultWwwBind)
}

func TestSampleConfigsBind(t *testing.T) {
	re := regexp.MustCompile(`(?m)^bind\s*=\s*(\S+)`)
	for _, f := range []string{"etc/zfswatcher.conf", "etc/zfswatcher-illumos.conf"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		m := re.FindSubmatch(b)
		if m == nil {
			t.Errorf("%s: no bind setting", f)
			continue
		}
		unprivilegedPort(t, string(m[1]))
	}
}
