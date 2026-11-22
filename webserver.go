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
	"fmt"
	"html/template"
	"math/rand"
	"net/http"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	auth "github.com/abbot/go-http-auth"
	"github.com/tantefred/zfswatcher/notifier"
)

var defaultPasswordInUse atomic.Bool

func getUserSecret(username, realm string) string {
	if username == "" {
		return ""
	}
	user, ok := cfg.Wwwuser[username]
	if !ok {
		return ""
	}
	if user.Enable {
		return user.Password
	}
	return ""
}

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

	authenticator := auth.NewBasicAuthenticator("zfswatcher", getUserSecret)

	tlsEnabled := cfg.Www.Certfile != "" && cfg.Www.Keyfile != ""
	srv := &http.Server{
		Addr:              cfg.Www.Bind,
		Handler:           newWebHandler(authenticator, tlsEnabled),
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
func newWebHandler(authenticator *auth.BasicAuth, tlsEnabled bool) http.Handler {
	mux := http.NewServeMux()

	mux.Handle(cfg.Www.Rootdir+"/resources/",
		http.StripPrefix(cfg.Www.Rootdir+"/resources",
			noDirListing(
				http.FileServer(
					http.Dir(cfg.Www.Resourcedir)))))

	mux.HandleFunc(cfg.Www.Rootdir+"/", authenticator.Wrap(dashboardHandler))
	mux.HandleFunc(cfg.Www.Rootdir+"/status/", authenticator.Wrap(statusHandler))
	mux.HandleFunc(cfg.Www.Rootdir+"/usage/", authenticator.Wrap(usageHandler))
	mux.HandleFunc(cfg.Www.Rootdir+"/statistics/", authenticator.Wrap(statisticsHandler))
	mux.HandleFunc(cfg.Www.Rootdir+"/logs/", authenticator.Wrap(logsHandler))
	mux.HandleFunc(cfg.Www.Rootdir+"/about/", authenticator.Wrap(aboutHandler))
	mux.HandleFunc(cfg.Www.Rootdir+"/locate/", authenticator.Wrap(locateHandler))
	mux.HandleFunc(cfg.Www.Rootdir+"/enclosure/", authenticator.Wrap(enclosureHandler))

	// Reject cross-origin state changing requests (CSRF) for all paths.
	return securityHeaders(http.NewCrossOriginProtection().Handler(mux), tlsEnabled)
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

// isDefaultPassword reports whether the MD5 crypt hash is the one of the
// well known example password.
func isDefaultPassword(hash string) bool {
	return checkMD5CryptHash(hash, "toor")
}

func checkMD5CryptHash(hash, password string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 4 || parts[0] != "" || parts[1] != "1" {
		return false
	}
	got := auth.MD5Crypt([]byte(password), []byte(parts[2]), []byte("$1$"))
	return string(got) == hash
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

func wwwHashPassword() {
	fmt.Printf("Password (will echo): ")
	var password string
	_, err := fmt.Scanln(&password)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	base64alpha := "./ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	var salt []byte
	for i := 0; i < 8; i++ {
		salt = append(salt, base64alpha[rand.Intn(len(base64alpha))])
	}

	hash := auth.MD5Crypt([]byte(password), salt, []byte("$1$"))
	fmt.Println("Hash:", string(hash))
}

// eof
