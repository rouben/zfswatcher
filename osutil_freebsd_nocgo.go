//go:build freebsd && !cgo

//
// osutil_freebsd_nocgo.go
//
// Minimal stubs so that the FreeBSD build can be compiled and vetted without
// cgo (for example in CI). The real FreeBSD binary needs cgo, see
// osutil_freebsd.go.
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
	"errors"
	"time"
)

var errUnsupported = errors.New("not available without cgo on this platform")

func getSystemUptime() (time.Duration, error) {
	return 0, errUnsupported
}

func getSystemLoadaverage() ([3]float32, error) {
	return [3]float32{}, errUnsupported
}

var deviceLookupPaths = [...]string{"/dev"}

// eof
