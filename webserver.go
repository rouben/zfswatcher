//
// webserver.go
//
// Copyright © 2012-2013 Damicon Kraa Oy
//
// This file is part of zfswatcher.
//
// Zfswatcher is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Zfswatcher is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with zfswatcher. If not, see <http://www.gnu.org/licenses/>.
//

package main

import (
	"html/template"
	"net/http"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/tantefred/zfswatcher/notifier"
)

var defaultPasswordInUse atomic.Bool

func noDirListing(h http.Handler) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func webServer() {
	var err error

	templates = template.New("zfswatcher").Funcs(template.FuncMap{
		"nicenumber": niceNumber,
	})
	templates, err = templates.ParseGlob(cfg.Www.Templatedir + "/*.html")
	if err != nil {
		notify.Printf(notifier.ERR, "error parsing templates: %s", err)
	}

	warnDefaultPassword()
	warnLegacyHashes()

	tlsEnabled := cfg.Www.Certfile != "" && cfg.Www.Keyfile != ""
	srv := &http.Server{
		Addr:              cfg.Www.Bind,
		Handler:           newWebHandler(tlsEnabled),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
	if tlsEnabled {
		err = srv.ListenAndServeTLS(cfg.Www.Certfile, cfg.Www.Keyfile)
	} else {
		err = srv.ListenAndServe()
	}
	if err != nil {
		notify.Printf(notifier.ERR, "error starting web server: %s", err)
	}
}

// newWebHandler builds the complete handler tree of the web interface.
func newWebHandler(tlsEnabled bool) http.Handler {
	mux := http.NewServeMux()

	mux.Handle(cfg.Www.Rootdir+"/resources/",
		http.StripPrefix(cfg.Www.Rootdir+"/resources",
			noDirListing(
				http.FileServer(
					http.Dir(cfg.Www.Resourcedir)))))

	// Health check for monitoring tools: deliberately without authentication.
	mux.HandleFunc(cfg.Www.Rootdir+"/healthz", healthzHandler)

	mux.HandleFunc(cfg.Www.Rootdir+"/", basicAuth(dashboardHandler))
	mux.HandleFunc(cfg.Www.Rootdir+"/status/", basicAuth(statusHandler))
	mux.HandleFunc(cfg.Www.Rootdir+"/usage/", basicAuth(usageHandler))
	mux.HandleFunc(cfg.Www.Rootdir+"/statistics/", basicAuth(statisticsHandler))
	mux.HandleFunc(cfg.Www.Rootdir+"/logs/", basicAuth(logsHandler))
	mux.HandleFunc(cfg.Www.Rootdir+"/about/", basicAuth(aboutHandler))
	mux.HandleFunc(cfg.Www.Rootdir+"/locate/", basicAuth(locateHandler))
	mux.HandleFunc(cfg.Www.Rootdir+"/enclosure/", basicAuth(enclosureHandler))

	// Reject cross-origin state changing requests (CSRF) for all paths.
	return securityHeaders(http.NewCrossOriginProtection().Handler(mux), tlsEnabled)
}

// healthzHandler reports the health of the daemon only (no pool states, no
// error details): 200 when the last zpool status query succeeded and is not
// older than three refresh intervals, otherwise 503.
func healthzHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "405 Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	zpoolHealth.mutex.Lock()
	seen, ok, at := zpoolHealth.seen, zpoolHealth.ok, zpoolHealth.at
	zpoolHealth.mutex.Unlock()

	code, text := http.StatusOK, "ok"
	switch {
	case !seen:
		code, text = http.StatusServiceUnavailable, "starting"
	case !ok:
		code, text = http.StatusServiceUnavailable, "error"
	case time.Since(at) > 3*time.Duration(cfg.Main.Zpoolstatusrefresh)*time.Second:
		code, text = http.StatusServiceUnavailable, "stale"
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	if r.Method == http.MethodGet {
		_, _ = w.Write([]byte(text + "\n"))
	}
}

// webCSP restricts the pages to their own origin. Inline styles are still
// needed because the Bootstrap templates use style attributes.
const webCSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; frame-ancestors 'none'; base-uri 'self'; form-action 'self'"

// securityHeaders adds defensive response headers to every response.
func securityHeaders(h http.Handler, tlsEnabled bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hdr := w.Header()
		hdr.Set("X-Content-Type-Options", "nosniff")
		hdr.Set("X-Frame-Options", "DENY")
		hdr.Set("Referrer-Policy", "same-origin")
		hdr.Set("Content-Security-Policy", webCSP)
		if tlsEnabled {
			hdr.Set("Strict-Transport-Security", "max-age=31536000")
		}
		h.ServeHTTP(w, r)
	})
}

// isDefaultPassword reports whether the hash (bcrypt or MD5 crypt) is the one
// of the well known example password.
func isDefaultPassword(hash string) bool {
	return checkPassword(hash, "toor")
}

// enabledDefaultPasswordUsers returns the enabled web users which still use
// the example password.
func enabledDefaultPasswordUsers() []string {
	var users []string
	for name, u := range cfg.Wwwuser {
		if u.Enable && isDefaultPassword(u.Password) {
			users = append(users, name)
		}
	}
	sort.Strings(users)
	return users
}

// warnDefaultPassword logs a warning for every web user with the example
// password and remembers the fact for the pages.
func warnDefaultPassword() {
	users := enabledDefaultPasswordUsers()
	defaultPasswordInUse.Store(len(users) > 0)
	for _, name := range users {
		notify.Printf(notifier.WARNING,
			"web user %q uses the well-known example password; change it (zfswatcher -P)", name)
	}
}

// warnLegacyHashes logs a warning for every enabled web user whose password
// is still stored as (deprecated) MD5 crypt hash.
func warnLegacyHashes() {
	var names []string
	for name, u := range cfg.Wwwuser {
		if u.Enable && isMD5CryptHash(u.Password) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		notify.Printf(notifier.WARNING,
			"web user %q: MD5 hash is deprecated, create a new one with zfswatcher -P", name)
	}
}

// eof
