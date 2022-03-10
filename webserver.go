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
	auth "github.com/abbot/go-http-auth"
	"zfswatcher/notifier"
	"html/template"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

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

	authenticator := auth.NewBasicAuthenticator("zfswatcher", getUserSecret)

	http.Handle(cfg.Www.Rootdir + "/resources/",
		http.StripPrefix(cfg.Www.Rootdir + "/resources",
			noDirListing(
					http.FileServer(
						http.Dir(cfg.Www.Resourcedir)))))

	http.HandleFunc(cfg.Www.Rootdir + "/", authenticator.Wrap(dashboardHandler))
	http.HandleFunc(cfg.Www.Rootdir + "/status/", authenticator.Wrap(statusHandler))
	http.HandleFunc(cfg.Www.Rootdir + "/usage/", authenticator.Wrap(usageHandler))
	http.HandleFunc(cfg.Www.Rootdir + "/statistics/", authenticator.Wrap(statisticsHandler))
	http.HandleFunc(cfg.Www.Rootdir + "/logs/", authenticator.Wrap(logsHandler))
	http.HandleFunc(cfg.Www.Rootdir + "/about/", authenticator.Wrap(aboutHandler))
	http.HandleFunc(cfg.Www.Rootdir + "/locate/", authenticator.Wrap(locateHandler))
	http.HandleFunc(cfg.Www.Rootdir + "/enclosure/", authenticator.Wrap(enclosureHandler))

	if cfg.Www.Certfile != "" && cfg.Www.Keyfile != "" {
		err = http.ListenAndServeTLS(cfg.Www.Bind, cfg.Www.Certfile, cfg.Www.Keyfile, nil)
	} else {
		err = http.ListenAndServe(cfg.Www.Bind, nil)
	}
	if err != nil {
		notify.Printf(notifier.ERR, "error starting web server: %s", err)
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
	rand.Seed(time.Now().UnixNano())

	base64alpha := "./ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	var salt []byte
	for i := 0; i < 8; i++ {
		salt = append(salt, base64alpha[rand.Intn(len(base64alpha))])
	}

	hash := auth.MD5Crypt([]byte(password), salt, []byte("$1$"))
	fmt.Println("Hash:", string(hash))
}

// eof
