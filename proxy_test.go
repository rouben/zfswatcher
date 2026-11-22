package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// probe returns the client IP and scheme seen by a handler behind proxyHeaders.
func probe(t *testing.T, trusted, remote string, hdr map[string]string) (ip, scheme string) {
	t.Helper()
	proxies, err := parseTrustedProxies(trusted)
	if err != nil {
		t.Fatal(err)
	}
	h := proxyHeaders(proxies, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, scheme = clientIP(r), clientScheme(r)
		if r.RemoteAddr != remote {
			t.Errorf("RemoteAddr changed to %q", r.RemoteAddr)
		}
	}))
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = remote
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	h.ServeHTTP(httptest.NewRecorder(), r)
	return
}

func TestProxyHeaders(t *testing.T) {
	tests := []struct {
		name, trusted, remote string
		hdr                   map[string]string
		ip, scheme            string
	}{
		{"empty list ignores headers", "", "127.0.0.1:1234",
			map[string]string{"X-Forwarded-For": "1.2.3.4", "X-Forwarded-Proto": "https", "X-Real-IP": "5.6.7.8"},
			"127.0.0.1", "http"},
		{"trusted proxy", "127.0.0.1", "127.0.0.1:1234",
			map[string]string{"X-Forwarded-For": "203.0.113.7", "X-Forwarded-Proto": "https"},
			"203.0.113.7", "https"},
		{"untrusted sender", "127.0.0.1", "198.51.100.9:1234",
			map[string]string{"X-Forwarded-For": "1.2.3.4", "X-Forwarded-Proto": "https"},
			"198.51.100.9", "http"},
		{"chain with several proxies", "10.0.0.0/8 192.168.1.1", "10.1.1.1:80",
			map[string]string{"X-Forwarded-For": "6.6.6.6, 203.0.113.7, 10.2.2.2, 192.168.1.1"},
			"203.0.113.7", "http"},
		{"forged leftmost entry is not used", "10.0.0.0/8", "10.0.0.1:80",
			map[string]string{"X-Forwarded-For": "6.6.6.6, 203.0.113.7"},
			"203.0.113.7", "http"},
		{"all hops trusted", "10.0.0.0/8", "10.0.0.1:80",
			map[string]string{"X-Forwarded-For": "10.0.0.5, 10.0.0.6"},
			"10.0.0.5", "http"},
		{"IPv6 proxy and client", "::1 2001:db8::/32", "[::1]:5555",
			map[string]string{"X-Forwarded-For": "2001:db8:1::5, 2001:db9::7"},
			"2001:db9::7", "http"},
		{"IPv4-mapped peer", "127.0.0.1", "[::ffff:127.0.0.1]:5555",
			map[string]string{"X-Forwarded-For": "203.0.113.7"},
			"203.0.113.7", "http"},
		{"garbage entry stops the walk", "127.0.0.1", "127.0.0.1:1",
			map[string]string{"X-Forwarded-For": "1.2.3.4, bogus"},
			"127.0.0.1", "http"},
		{"X-Real-IP fallback", "127.0.0.1", "127.0.0.1:1",
			map[string]string{"X-Real-IP": "203.0.113.8"},
			"203.0.113.8", "http"},
		{"invalid proto ignored", "127.0.0.1", "127.0.0.1:1",
			map[string]string{"X-Forwarded-Proto": "javascript"},
			"127.0.0.1", "http"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ip, scheme := probe(t, tc.trusted, tc.remote, tc.hdr)
			if ip != tc.ip || scheme != tc.scheme {
				t.Errorf("got %s %s, want %s %s", ip, scheme, tc.ip, tc.scheme)
			}
		})
	}
}

func TestParseTrustedProxiesInvalid(t *testing.T) {
	for _, s := range []string{"nonsense", "10.0.0.0/33", "127.0.0.1 foo", "fe80::1%eth0", "1.2.3"} {
		if _, err := parseTrustedProxies(s); err == nil {
			t.Errorf("%q: expected error", s)
		}
	}
}

func TestGetCfgInvalidTrustedProxies(t *testing.T) {
	oldFile := cfgFile
	t.Cleanup(func() { cfgFile = oldFile })
	for conf, wantNil := range map[string]bool{
		"[www]\ntrustedproxies = 127.0.0.1 10.0.0.0/8 ::1\n": false,
		"[www]\ntrustedproxies = 127.0.0.1 bogus\n":          true,
	} {
		cfgFile = filepath.Join(t.TempDir(), "zfswatcher.conf")
		if err := os.WriteFile(cfgFile, []byte(conf), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := getCfg() == nil; got != wantNil {
			t.Errorf("%q: config rejected = %v, want %v", conf, got, wantNil)
		}
	}
}

// The CSRF check compares Origin with Host. Forged X-Forwarded-* headers
// must not influence it, not even from a trusted proxy.
func TestProxyDoesNotRelaxCSRF(t *testing.T) {
	setupWebTest(t, false)
	cfg.Www.Trustedproxies = "192.0.2.1"
	h := newWebHandler(false)
	r := httptest.NewRequest("POST", "/zw/locate/", strings.NewReader("dev=1-1&state=on"))
	r.Host = "zfs.example"
	r.RemoteAddr = "192.0.2.1:4444"
	r.SetBasicAuth("root", "toor")
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "https://evil.example")
	r.Header.Set("X-Forwarded-Host", "evil.example")
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-Forwarded-For", "203.0.113.7")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-origin POST via trusted proxy: got %d, want 403", w.Code)
	}
}

func TestHSTSFromForwardedProto(t *testing.T) {
	setupWebTest(t, false)
	cfg.Www.Trustedproxies = "192.0.2.1"
	h := newWebHandler(false)
	for remote, want := range map[string]string{"192.0.2.1:1": "max-age=31536000", "192.0.2.2:1": ""} {
		r := httptest.NewRequest("GET", "/zw/resources/x.css", nil)
		r.RemoteAddr = remote
		r.Header.Set("X-Forwarded-Proto", "https")
		r.SetBasicAuth("root", "toor")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if got := w.Header().Get("Strict-Transport-Security"); got != want {
			t.Errorf("%s: HSTS %q, want %q", remote, got, want)
		}
	}
}
