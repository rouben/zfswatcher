//
// md5crypt.go
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
	"crypto/md5" //nolint:gosec // only to verify legacy "$1$" hashes in existing configurations
	"strings"
)

const md5CryptAlphabet = "./0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// md5CryptOrder is the byte order of the final digest in the crypt encoding.
var md5CryptOrder = [16]int{12, 6, 0, 13, 7, 1, 14, 8, 2, 15, 9, 3, 5, 10, 4, 11}

// md5Crypt computes the classic MD5 based "$1$salt$hash" password hash
// (algorithm of Poul-Henning Kamp, same as "openssl passwd -1"). It is only
// used to verify hashes of existing configurations; new hashes are created
// with bcrypt.
func md5Crypt(password, salt string) string {
	if len(salt) > 8 {
		salt = salt[:8]
	}
	pw, sl := []byte(password), []byte(salt)

	alt := md5.New()
	alt.Write(pw)
	alt.Write(sl)
	alt.Write(pw)
	mix := alt.Sum(nil)

	d := md5.New()
	d.Write(pw)
	d.Write([]byte("$1$"))
	d.Write(sl)
	for n := len(pw); n > 0; n -= 16 {
		d.Write(mix[:min(n, 16)])
	}
	for n := len(pw); n > 0; n >>= 1 {
		if n&1 != 0 {
			d.Write([]byte{0})
		} else {
			d.Write(pw[:1])
		}
	}
	final := d.Sum(nil)

	for i := 0; i < 1000; i++ {
		d = md5.New()
		if i&1 != 0 {
			d.Write(pw)
		} else {
			d.Write(final)
		}
		if i%3 != 0 {
			d.Write(sl)
		}
		if i%7 != 0 {
			d.Write(pw)
		}
		if i&1 != 0 {
			d.Write(final)
		} else {
			d.Write(pw)
		}
		final = d.Sum(nil)
	}

	var out strings.Builder
	out.WriteString("$1$" + salt + "$")
	var v, bits uint
	for _, i := range md5CryptOrder {
		v |= uint(final[i]) << bits
		for bits += 8; bits > 6; bits -= 6 {
			out.WriteByte(md5CryptAlphabet[v&0x3f])
			v >>= 6
		}
	}
	out.WriteByte(md5CryptAlphabet[v&0x3f])
	return out.String()
}
