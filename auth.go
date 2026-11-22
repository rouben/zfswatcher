//
// auth.go
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
	"crypto/subtle"
	"net/http"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"github.com/tantefred/zfswatcher/notifier"
)

const authRealm = "zfswatcher"

// dummyBcryptHash is compared against when the user is unknown or disabled,
// so that the response time does not reveal which user names exist. The
// password it was made from is irrelevant (and never matches).
const dummyBcryptHash = "$2y$10$UixQGmoY3RV64c3a.CNpOuBDX31Sr9dYBuRAs0cchTcDXazwH0f1i"

func isBcryptHash(hash string) bool {
	for _, p := range []string{"$2a$", "$2b$", "$2y$"} {
		if strings.HasPrefix(hash, p) {
			return true
		}
	}
	return false
}

func isMD5CryptHash(hash string) bool {
	return strings.HasPrefix(hash, "$1$")
}

// checkPassword verifies the password against a bcrypt ("$2a$", "$2b$",
// "$2y$") or MD5 crypt ("$1$") hash. Other formats never match.
func checkPassword(hash, password string) bool {
	switch {
	case isBcryptHash(hash):
		return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
	case isMD5CryptHash(hash):
		parts := strings.Split(hash, "$")
		if len(parts) != 4 || parts[2] == "" {
			return false
		}
		return subtle.ConstantTimeCompare([]byte(md5Crypt(password, parts[2])), []byte(hash)) == 1
	}
	return false
}

// authenticate checks the credentials against the enabled web users.
func authenticate(username, password string) bool {
	hash := ""
	if user, ok := cfg.Wwwuser[username]; ok && user.Enable {
		hash = user.Password
	}
	if hash == "" || (!isBcryptHash(hash) && !isMD5CryptHash(hash)) {
		// burn the same time as a real check
		_ = bcrypt.CompareHashAndPassword([]byte(dummyBcryptHash), []byte(password))
		return false
	}
	return checkPassword(hash, password)
}

// basicAuth wraps the handler with HTTP Basic authentication.
func basicAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || !authenticate(username, password) {
			if ok && notify != nil {
				notify.Printf(notifier.WARNING, "web login failed for user %q from %s", username, clientIP(r))
			}
			w.Header().Set("WWW-Authenticate", `Basic realm="`+authRealm+`", charset="UTF-8"`)
			http.Error(w, "401 Unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}
