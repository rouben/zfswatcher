package main

import (
	"net"
	"os"
	"path/filepath"
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

func TestCheckCommandTimeout(t *testing.T) {
	for _, c := range []struct {
		sec int
		ok  bool
	}{{0, false}, {-1, false}, {1, true}, {60, true}, {3600, true}, {3601, false}} {
		if err := checkCommandTimeout(c.sec); (err == nil) != c.ok {
			t.Errorf("checkCommandTimeout(%d) = %v, ok want %v", c.sec, err, c.ok)
		}
	}
}

func cfgWithMain(t *testing.T, body string) *cfgType {
	t.Helper()
	old := cfgFile
	t.Cleanup(func() { cfgFile = old })
	cfgFile = filepath.Join(t.TempDir(), "z.conf")
	if err := os.WriteFile(cfgFile, []byte("[main]\n"+body), 0o600); err != nil {
		t.Fatal(err)
	}
	return getCfg()
}

func TestGetCfgCommandTimeout(t *testing.T) {
	if c := cfgWithMain(t, ""); c == nil || c.Main.Commandtimeout != 60 {
		t.Errorf("default: got %+v, want 60", c)
	}
	if c := cfgWithMain(t, "commandtimeout = 120\n"); c == nil || c.Main.Commandtimeout != 120 {
		t.Errorf("valid value: got %+v, want 120", c)
	}
	for _, v := range []string{"0", "3601", "-5"} {
		if cfgWithMain(t, "commandtimeout = "+v+"\n") != nil {
			t.Errorf("getCfg accepted commandtimeout = %s", v)
		}
	}
}
