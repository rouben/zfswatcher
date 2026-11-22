package main

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

const (
	testToorHash       = "$1$fNF5/E3q$TEYvY1AQae/7rzHQ5fVgD1"
	testToorBcryptHash = "$2y$05$k43gKQyMPSXtKfJTayqCXuNNuPBMoSIoKt59Qf1c5hPgkJC68WoWC"
)

func setupWebTest(t *testing.T, tls bool) http.Handler {
	t.Helper()
	old := cfg
	cfg = &cfgType{}
	t.Cleanup(func() { cfg = old })
	cfg.Www.Rootdir = "/zw"
	cfg.Www.Resourcedir = t.TempDir()
	cfg.Wwwuser = map[string]*struct {
		Enable   bool
		Password string
	}{"root": {Enable: true, Password: testToorHash}}
	devLedsMutex.Lock()
	devLeds = map[string]*devLed{"1-1": {}}
	devLedsMutex.Unlock()
	t.Cleanup(func() {
		devLedsMutex.Lock()
		devLeds = nil
		devLedsMutex.Unlock()
	})
	return newWebHandler(tls)
}

func doReq(h http.Handler, method, path string, form url.Values, hdr map[string]string) *httptest.ResponseRecorder {
	body := strings.NewReader("")
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	r := httptest.NewRequest(method, path, body)
	r.Host = "zfs.example"
	r.SetBasicAuth("root", "toor")
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestLocateGetNotAllowed(t *testing.T) {
	h := setupWebTest(t, false)
	w := doReq(h, "GET", "/zw/locate/?dev=1-1&state=on", nil, nil)
	if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != "POST" {
		t.Fatalf("got %d, Allow=%q", w.Code, w.Header().Get("Allow"))
	}
}

func TestLocateCrossOriginRejected(t *testing.T) {
	h := setupWebTest(t, false)
	form := url.Values{"dev": {"1-1"}, "state": {"on"}}
	w := doReq(h, "POST", "/zw/locate/", form, map[string]string{"Origin": "https://evil.example"})
	if w.Code != http.StatusForbidden {
		t.Fatalf("Origin: got %d", w.Code)
	}
	w = doReq(h, "POST", "/zw/locate/", form, map[string]string{"Sec-Fetch-Site": "cross-site"})
	if w.Code != http.StatusForbidden {
		t.Fatalf("Sec-Fetch-Site: got %d", w.Code)
	}
}

func TestLocateUnknownDevice(t *testing.T) {
	h := setupWebTest(t, false)
	for _, dev := range []string{"", "nonexistent", "../../etc/passwd"} {
		form := url.Values{"dev": {dev}, "state": {"on"}}
		w := doReq(h, "POST", "/zw/locate/", form, map[string]string{"Origin": "http://zfs.example"})
		if w.Code != http.StatusBadRequest {
			t.Errorf("dev %q: got %d", dev, w.Code)
		}
	}
}

func TestLocateRedirectInternal(t *testing.T) {
	h := setupWebTest(t, false)
	cases := map[string]string{
		"":                         "/zw/enclosure/",
		"/zw/status/tank":          "/zw/status/tank",
		"https://evil.example/":    "/zw/enclosure/",
		"//evil.example/zw/":       "/zw/enclosure/",
		"/zw//evil.example":        "/zw/enclosure/",
		"/other/page":              "/zw/enclosure/",
		"/zw/\\evil.example":       "/zw/enclosure/",
		"/zw/x\r\nSet-Cookie: a=b": "/zw/enclosure/",
	}
	for ret, want := range cases {
		form := url.Values{"dev": {"1-1"}, "state": {"off"}, "return": {ret}}
		w := doReq(h, "POST", "/zw/locate/", form, map[string]string{"Origin": "http://zfs.example"})
		if w.Code != http.StatusSeeOther {
			t.Fatalf("return %q: got %d", ret, w.Code)
		}
		if loc := w.Header().Get("Location"); loc != want {
			t.Errorf("return %q: Location %q, want %q", ret, loc, want)
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	for _, tls := range []bool{false, true} {
		h := setupWebTest(t, tls)
		w := doReq(h, "GET", "/zw/resources/missing.css", nil, nil)
		for _, k := range []string{"X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy", "Content-Security-Policy"} {
			if w.Header().Get(k) == "" {
				t.Errorf("tls=%v: header %s missing", tls, k)
			}
		}
		if got := w.Header().Get("Strict-Transport-Security") != ""; got != tls {
			t.Errorf("tls=%v: HSTS present=%v", tls, got)
		}
		// an unauthenticated request (401) carries the headers as well
		r := httptest.NewRequest("GET", "/zw/about/", nil)
		w = httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized || w.Header().Get("X-Frame-Options") != "DENY" {
			t.Errorf("tls=%v: code %d, X-Frame-Options %q", tls, w.Code, w.Header().Get("X-Frame-Options"))
		}
	}
}

func TestDefaultPasswordDetection(t *testing.T) {
	if !isDefaultPassword(testToorHash) {
		t.Error("toor hash not detected")
	}
	if !isDefaultPassword(testToorBcryptHash) {
		t.Error("bcrypt toor hash not detected")
	}
	for _, h := range []string{"", "toor", "$1$dlPL2MqE$oQmn16q49SqdmhenQuNgs1", "$1$fNF5/E3q$bad", "$5$x$y",
		"$2y$05$qEpySWOaHbtq76BTs9NEj.92DGdwHrqQuNRFMd7GX6SY/hGLRLxnK", "$2y$05$broken"} {
		if isDefaultPassword(h) {
			t.Errorf("%q wrongly detected", h)
		}
	}
	setupWebTest(t, false)
	if u := enabledDefaultPasswordUsers(); len(u) != 1 || u[0] != "root" {
		t.Errorf("users = %v", u)
	}
	cfg.Wwwuser["root"].Enable = false
	if u := enabledDefaultPasswordUsers(); len(u) != 0 {
		t.Errorf("disabled user listed: %v", u)
	}
}

func TestDefaultPasswordBanner(t *testing.T) {
	tpl, err := template.New("t").Funcs(template.FuncMap{"nicenumber": niceNumber}).ParseGlob("www/templates/*.html")
	if err != nil {
		t.Fatal(err)
	}
	defer defaultPasswordInUse.Store(false)
	for _, inUse := range []bool{true, false} {
		defaultPasswordInUse.Store(inUse)
		var b strings.Builder
		if err := tpl.ExecuteTemplate(&b, "header.html", webNav{Root: "/zw"}); err != nil {
			t.Fatal(err)
		}
		if got := strings.Contains(b.String(), "alert alert-danger"); got != inUse {
			t.Errorf("inUse=%v: banner present=%v", inUse, got)
		}
	}
}

func setHealth(t *testing.T, seen, ok bool, age time.Duration) {
	t.Helper()
	zpoolHealth.mutex.Lock()
	zpoolHealth.seen, zpoolHealth.ok, zpoolHealth.at = seen, ok, time.Now().Add(-age)
	zpoolHealth.mutex.Unlock()
	t.Cleanup(func() {
		zpoolHealth.mutex.Lock()
		zpoolHealth.seen, zpoolHealth.ok = false, false
		zpoolHealth.mutex.Unlock()
	})
}

// doReqNoAuth sends a request without credentials.
func doReqNoAuth(h http.Handler, method, path string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	r.Host = "zfs.example"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestHealthz(t *testing.T) {
	h := setupWebTest(t, false)
	cfg.Main.Zpoolstatusrefresh = 10
	tests := []struct {
		name     string
		seen, ok bool
		age      time.Duration
		code     int
		body     string
	}{
		{"starting", false, false, 0, 503, "starting\n"},
		{"ok", true, true, 5 * time.Second, 200, "ok\n"},
		{"stale", true, true, 31 * time.Second, 503, "stale\n"},
		{"error", true, false, time.Second, 503, "error\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setHealth(t, tc.seen, tc.ok, tc.age)
			w := doReqNoAuth(h, "GET", "/zw/healthz")
			if w.Code != tc.code || w.Body.String() != tc.body {
				t.Fatalf("got %d %q", w.Code, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store" ||
				w.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatalf("headers: %v", w.Header())
			}
		})
	}
}

func TestHealthzMethods(t *testing.T) {
	h := setupWebTest(t, false)
	cfg.Main.Zpoolstatusrefresh = 10
	setHealth(t, true, true, 0)
	w := doReqNoAuth(h, "HEAD", "/zw/healthz")
	if w.Code != http.StatusOK || w.Body.Len() != 0 {
		t.Fatalf("HEAD: got %d %q", w.Code, w.Body.String())
	}
	w = doReqNoAuth(h, "POST", "/zw/healthz")
	if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("POST: got %d, Allow=%q", w.Code, w.Header().Get("Allow"))
	}
}

func TestHealthzWithoutAuth(t *testing.T) {
	h := setupWebTest(t, false)
	cfg.Main.Zpoolstatusrefresh = 10
	setHealth(t, true, true, 0)
	if w := doReqNoAuth(h, "GET", "/zw/healthz"); w.Code != http.StatusOK {
		t.Fatalf("healthz: got %d", w.Code)
	}
	if w := doReqNoAuth(h, "GET", "/zw/about/"); w.Code != http.StatusUnauthorized {
		t.Fatalf("about: got %d", w.Code)
	}
}
