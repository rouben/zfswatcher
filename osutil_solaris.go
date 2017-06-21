//
// osutil_solaris.go
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

// +build solaris

package main

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Returns system uptime as time.Duration.
func getSystemUptime() (uptime time.Duration, err error) {
	var (
		i    int
		out  []byte
		err1 error
		err2 error
	)

	cmd := "kstat"
	args := []string{"-p", "unix:0:system_misc:snaptime"}
	if out, err1 = exec.Command(cmd, args...).Output(); err1 != nil {
		return 0, errors.New("Failed to run 'kstat'")
	}
	_s := string(out)
	parts := strings.Split(_s, "\t")
	_p1 := parts[1]
	_s = strings.Split(_p1, ".")[0]
	//fmt.Printf("Before the dot -> %s\n", _s)
	if i, err2 = strconv.Atoi(_s); err2 != nil {
		return 0, errors.New("Uptime was not an integer")
	}
	uptime = time.Duration(i) * time.Second
	return uptime, nil
}

// Returns system load averages.
func getSystemLoadaverage() ([3]float32, error) {
	var (
		out []byte
		err error
		ret [3]float32
		val float64
	)
	cmd := "uptime"
	if out, err = exec.Command(cmd).Output(); err != nil {
		return ret, errors.New("failed to run 'uptime'")
	}
	_s := string(out)
	parts := strings.Split(_s, " ")
	load := parts[len(parts)-3:]
	//fmt.Printf("load parts -> %s\n", load)
	for i, e := range load {
		e = strings.Replace(e, ",", "", 1)
		e = strings.Join(strings.Fields(e), "")
		if val, err = strconv.ParseFloat(e, 32); err != nil {
			return ret, errors.New(fmt.Sprintf("failed to convert load value '%s'", e))
		}
		ret[i] = float32(val)
	}
	return ret, nil
	//return [3]float32{0, 0, 0}, nil
}

// Device lookup paths. (This list comes from lib/libzfs/libzfs_import.c)
var deviceLookupPaths = [...]string{
	"/dev/dsk",
}

// eof
