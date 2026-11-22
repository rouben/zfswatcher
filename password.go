//
// password.go
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
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
)

// readPassword reads a password from in. On a terminal it is read without
// echo and asked for twice; otherwise one line is read (for scripts).
func readPassword(in *os.File, prompt io.Writer) (string, error) {
	if term.IsTerminal(int(in.Fd())) {
		_, _ = fmt.Fprint(prompt, "Password: ")
		p1, err := term.ReadPassword(int(in.Fd()))
		_, _ = fmt.Fprintln(prompt)
		if err != nil {
			return "", err
		}
		_, _ = fmt.Fprint(prompt, "Repeat password: ")
		p2, err := term.ReadPassword(int(in.Fd()))
		_, _ = fmt.Fprintln(prompt)
		if err != nil {
			return "", err
		}
		if string(p1) != string(p2) {
			return "", errors.New("passwords do not match")
		}
		return checkNewPassword(string(p1))
	}
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && (err != io.EOF || line == "") {
		return "", errors.New("no password on standard input")
	}
	return checkNewPassword(strings.TrimRight(line, "\r\n"))
}

func checkNewPassword(p string) (string, error) {
	if p == "" {
		return "", errors.New("empty password not allowed")
	}
	return p, nil
}

// wwwHashPassword implements "zfswatcher -P": it reads a password and prints
// its bcrypt hash. The cost is bcrypt.DefaultCost (10): the hash is verified
// on every request, so higher values make page loads noticeably slow on small
// machines.
func wwwHashPassword(in *os.File, out, errOut io.Writer) int {
	password, err := readPassword(in, errOut)
	if err != nil {
		_, _ = fmt.Fprintln(errOut, "Error:", err)
		return 1
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		_, _ = fmt.Fprintln(errOut, "Error:", err)
		return 1
	}
	_, _ = fmt.Fprintln(out, string(hash))
	return 0
}
