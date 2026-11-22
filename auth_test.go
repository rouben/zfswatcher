package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestMD5Crypt(t *testing.T) {
	// vectors from the existing configuration and "openssl passwd -1 -salt"
	cases := []struct{ password, salt, want string }{
		{"toor", "fNF5/E3q", "$1$fNF5/E3q$TEYvY1AQae/7rzHQ5fVgD1"},
		{"hello", "dlPL2MqE", "$1$dlPL2MqE$oQmn16q49SqdmhenQuNgs1"},
		{"toor", "abcdefgh", "$1$abcdefgh$pK0Bp1VYQIo78LEcSA7zK0"},
		{"hello", "abcdefgh", "$1$abcdefgh$rwnEbRiN0agqVgZBovWNQ/"},
		{"", "abcdefgh", "$1$abcdefgh$M55TzYaaccxVGbptZWaxX/"},
		{"a long password with spaces and more than sixteen chars", "abcdefgh", "$1$abcdefgh$V/cui7inUsc.WIM1V3DFK0"},
		{"ünï", "abcdefgh", "$1$abcdefgh$P/Bbp4nmFvuss19s.OpgK0"},
		{"x", "ab", "$1$ab$e2KlfqG5YBMTjSz7XF.Eu1"},
	}
	for _, c := range cases {
		if got := md5Crypt(c.password, c.salt); got != c.want {
			t.Errorf("md5Crypt(%q, %q) = %s, want %s", c.password, c.salt, got, c.want)
		}
	}
	// a salt longer than 8 characters is truncated
	if got := md5Crypt("toor", "abcdefghXXXX"); got != "$1$abcdefgh$pK0Bp1VYQIo78LEcSA7zK0" {
		t.Errorf("long salt: %s", got)
	}
}

func TestCheckPassword(t *testing.T) {
	cases := []struct {
		hash, password string
		want           bool
	}{
		{testToorHash, "toor", true},
		{testToorHash, "Toor", false},
		{testToorBcryptHash, "toor", true},
		{testToorBcryptHash, "hello", false},
		{strings.Replace(testToorBcryptHash, "$2y$", "$2a$", 1), "toor", true},
		{strings.Replace(testToorBcryptHash, "$2y$", "$2b$", 1), "toor", true},
		{"", "", false},
		{"toor", "toor", false},
		{"$1$$x", "toor", false},
		{"$5$salt$hash", "toor", false},
		{"$2y$05$short", "toor", false},
	}
	for _, c := range cases {
		if got := checkPassword(c.hash, c.password); got != c.want {
			t.Errorf("checkPassword(%q, %q) = %v", c.hash, c.password, got)
		}
	}
}

func TestBasicAuth(t *testing.T) {
	old := cfg
	cfg = &cfgType{}
	t.Cleanup(func() { cfg = old })
	cfg.Wwwuser = map[string]*struct {
		Enable   bool
		Password string
	}{
		"md5":      {Enable: true, Password: testToorHash},
		"bcrypt":   {Enable: true, Password: testToorBcryptHash},
		"disabled": {Enable: false, Password: testToorBcryptHash},
		"nohash":   {Enable: true, Password: ""},
		"plain":    {Enable: true, Password: "toor"},
	}
	h := basicAuth(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	cases := []struct {
		name, user, pass string
		noHeader         bool
		want             int
	}{
		{"no header", "", "", true, 401},
		{"wrong password", "bcrypt", "wrong", false, 401},
		{"wrong password md5", "md5", "wrong", false, 401},
		{"unknown user", "nobody", "toor", false, 401},
		{"disabled user", "disabled", "toor", false, 401},
		{"empty hash", "nohash", "", false, 401},
		{"plain text hash", "plain", "toor", false, 401},
		{"bcrypt", "bcrypt", "toor", false, 200},
		{"md5", "md5", "toor", false, 200},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		if !c.noHeader {
			r.SetBasicAuth(c.user, c.pass)
		}
		w := httptest.NewRecorder()
		h(w, r)
		if w.Code != c.want {
			t.Errorf("%s: got %d, want %d", c.name, w.Code, c.want)
		}
		if c.want == 401 && !strings.HasPrefix(w.Header().Get("WWW-Authenticate"), `Basic realm="zfswatcher"`) {
			t.Errorf("%s: WWW-Authenticate %q", c.name, w.Header().Get("WWW-Authenticate"))
		}
	}
}

func stdinFile(t *testing.T, content string) *os.File {
	t.Helper()
	f, err := os.Create(filepath.Join(t.TempDir(), "stdin"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestReadPasswordNonTerminal(t *testing.T) {
	for in, want := range map[string]string{"secret\n": "secret", "secret": "secret", "a b\r\n": "a b", "first\nsecond\n": "first"} {
		if got, err := readPassword(stdinFile(t, in), &strings.Builder{}); err != nil || got != want {
			t.Errorf("%q: got %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "\n", "\r\n"} {
		if _, err := readPassword(stdinFile(t, in), &strings.Builder{}); err == nil {
			t.Errorf("%q: expected error", in)
		}
	}
}

func TestWwwHashPassword(t *testing.T) {
	var out, errOut strings.Builder
	if rc := wwwHashPassword(stdinFile(t, "s3cret\n"), &out, &errOut); rc != 0 {
		t.Fatalf("rc %d: %s", rc, errOut.String())
	}
	hash := strings.TrimSpace(out.String())
	if !strings.HasPrefix(hash, "$2a$10$") || bcrypt.CompareHashAndPassword([]byte(hash), []byte("s3cret")) != nil {
		t.Errorf("bad hash %q", hash)
	}
	out.Reset()
	if rc := wwwHashPassword(stdinFile(t, "\n"), &out, &errOut); rc == 0 || out.Len() != 0 {
		t.Errorf("empty password accepted: rc %d", rc)
	}
}
