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
	"context"
	"fmt"
	"html/template"
	"net/http"
	"net/netip"
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

	mux.HandleFunc(cfg.Www.Rootdir+"/", basicAuth(dashboardHandler))
	mux.HandleFunc(cfg.Www.Rootdir+"/status/", basicAuth(statusHandler))
	mux.HandleFunc(cfg.Www.Rootdir+"/usage/", basicAuth(usageHandler))
	mux.HandleFunc(cfg.Www.Rootdir+"/statistics/", basicAuth(statisticsHandler))
	mux.HandleFunc(cfg.Www.Rootdir+"/logs/", basicAuth(logsHandler))
	mux.HandleFunc(cfg.Www.Rootdir+"/about/", basicAuth(aboutHandler))
	mux.HandleFunc(cfg.Www.Rootdir+"/locate/", basicAuth(locateHandler))
	mux.HandleFunc(cfg.Www.Rootdir+"/enclosure/", basicAuth(enclosureHandler))

	// Reject cross-origin state changing requests (CSRF) for all paths.
	// The proxy handling runs first so that everything below sees the
	// client address and scheme. It does not touch Host, which the CSRF
	// check compares with the Origin header (see README).
	proxies, _ := parseTrustedProxies(cfg.Www.Trustedproxies) // validated in getCfg
	return proxyHeaders(proxies, securityHeaders(http.NewCrossOriginProtection().Handler(mux), tlsEnabled))
}

// parseTrustedProxies parses a space separated list of IP addresses and
// CIDR networks. An empty list means that no proxy is trusted.
func parseTrustedProxies(list string) ([]netip.Prefix, error) {
	var res []netip.Prefix
	for _, f := range strings.Fields(list) {
		if p, err := netip.ParsePrefix(f); err == nil {
			if p.Addr().Zone() != "" {
				return nil, fmt.Errorf("invalid IP address or network %q", f)
			}
			q := netip.PrefixFrom(p.Addr().Unmap(), unmapBits(p))
			if !q.IsValid() {
				return nil, fmt.Errorf("invalid IP address or network %q", f)
			}
			res = append(res, q.Masked())
			continue
		}
		a, err := netip.ParseAddr(f)
		if err != nil || a.Zone() != "" {
			return nil, fmt.Errorf("invalid IP address or network %q", f)
		}
		a = a.Unmap()
		res = append(res, netip.PrefixFrom(a, a.BitLen()))
	}
	return res, nil
}

// unmapBits returns the prefix length for p after its address was converted
// from an IPv4-mapped IPv6 address to IPv4.
func unmapBits(p netip.Prefix) int {
	if p.Addr().Is4In6() && p.Bits() >= 96 {
		return p.Bits() - 96
	}
	return p.Bits()
}

func isTrusted(proxies []netip.Prefix, a netip.Addr) bool {
	for _, p := range proxies {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

type clientInfoKey struct{}

// clientInfo is the client address and scheme as seen by the web server,
// taking trusted reverse proxies into account.
type clientInfo struct {
	IP     string
	Scheme string
}

// clientIP returns the client address of the request: the real peer, or, if
// that is a trusted proxy, the address forwarded by it.
func clientIP(r *http.Request) string {
	if ci, ok := r.Context().Value(clientInfoKey{}).(clientInfo); ok {
		return ci.IP
	}
	return r.RemoteAddr
}

// clientScheme returns "https" or "http" as used by the client.
func clientScheme(r *http.Request) string {
	if ci, ok := r.Context().Value(clientInfoKey{}).(clientInfo); ok {
		return ci.Scheme
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

// proxyHeaders evaluates X-Forwarded-For and X-Forwarded-Proto,
// but only if the direct peer is a trusted proxy; otherwise (and always with
// an empty list) the headers are ignored, because any client can forge them.
// The result is stored in the request context; r.RemoteAddr stays untouched.
func proxyHeaders(proxies []netip.Prefix, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ci := clientInfo{IP: r.RemoteAddr, Scheme: "http"}
		if r.TLS != nil {
			ci.Scheme = "https"
		}
		if ap, err := netip.ParseAddrPort(r.RemoteAddr); err == nil {
			peer := ap.Addr().Unmap()
			ci.IP = peer.String()
			if isTrusted(proxies, peer) {
				ci.IP = forwardedClient(proxies, peer, r.Header)
				// the rightmost value was added by the nearest proxy
				pv := r.Header.Values("X-Forwarded-Proto")
				if len(pv) > 0 {
					parts := strings.Split(pv[len(pv)-1], ",")
					switch v := strings.ToLower(strings.TrimSpace(parts[len(parts)-1])); v {
					case "http", "https":
						ci.Scheme = v
					}
				}
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientInfoKey{}, ci)))
	})
}

// forwardedClient walks X-Forwarded-For from right to left and returns the
// first address which is not a trusted proxy. Unparsable entries end the
// walk, the last good hop wins.
func forwardedClient(proxies []netip.Prefix, peer netip.Addr, h http.Header) string {
	var hops []string
	for _, v := range h.Values("X-Forwarded-For") {
		hops = append(hops, strings.Split(v, ",")...)
	}
	if len(hops) == 0 {
		return peer.String()
	}
	client := peer
	for i := len(hops) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			break
		}
		client = a.Unmap().WithZone("")
		if !isTrusted(proxies, client) {
			break
		}
	}
	return client.String()
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
		if tlsEnabled || clientScheme(r) == "https" {
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
