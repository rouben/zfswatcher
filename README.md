[![Language](https://img.shields.io/badge/language-Go-brightgreen.svg)](https://golang.org/)
![OS](https://img.shields.io/badge/os-Linux%2C%20FreeBSD%2C%20Solaris%2C%20OpenSolaris%2C%20OpenIndiana-brightgreen.svg)
[![License](https://img.shields.io/github/license/tantefred/zfswatcher.svg)](COPYING)
[![Release](https://img.shields.io/github/release/tantefred/zfswatcher.svg)](https://github.com/tantefred/zfswatcher/releases)
[![GitHub issues](https://img.shields.io/github/issues/tantefred/zfswatcher.svg)](https://github.com/tantefred/zfswatcher/issues)
[![GitHub forks](https://img.shields.io/github/forks/tantefred/zfswatcher.svg)](https://github.com/tantefred/zfswatcher/network)
[![GitHub stars](https://img.shields.io/github/stars/tantefred/zfswatcher.svg)](https://github.com/tantefred/zfswatcher/stargazers)

# zfswatcher

ZFS pool monitoring and notification daemon.

Zfswatcher watches the state of your ZFS pools and notifies you about
problems by e-mail, syslog or log file. It can also manage enclosure LEDs
and provides an optional embedded web interface.

Supported operating systems: Linux, FreeBSD and Solaris/Illumos
(OpenIndiana).

> **About this fork:** This repository is a fork of
> [rouben/zfswatcher](https://github.com/rouben/zfswatcher), which itself is
> based on the original [damicon/zfswatcher](https://github.com/damicon/zfswatcher)
> by Janne Snabb (Damicon Kraa Oy). See [Credits](#credits).


## Installing and upgrading on Debian/Ubuntu

Download the `.deb` package from the
[releases page](https://github.com/tantefred/zfswatcher/releases) and install
it with `dpkg`, for example:

```sh
dpkg -i zfswatcher_0.6.0-1_amd64.deb
```

The service is started by default according to the Debian and Ubuntu
conventions. It runs as the unprivileged system user `zfswatcher` (created by
the package); see "Running as root" below if you need that for LED control.

When upgrading from 0.4.x: the daemon no longer runs as root, the SysV init
script is replaced by a systemd unit, and `/var/log/zfswatcher/` is owned by
the new user. Add `SUSPENDED:alert` to `poolstatemap` in your own
`/etc/zfs/zfswatcher.conf` (see `NEWS`).

Development builds are published automatically from the `contrib` branch as
the `latest-dev` pre-release.


## Installing and upgrading on RHEL/CentOS/Scientific Linux

Download the `.rpm` package from the
[releases page](https://github.com/tantefred/zfswatcher/releases) and install
it with `yum`, for example:

```sh
yum install zfswatcher-0.6.0-1.x86_64.rpm
```

The service is **not** started by default according to Red Hat
conventions. It can be started as follows:

```sh
service zfswatcher start
```


## Installing from source on Linux, FreeBSD and Solaris/Illumos

Generally it is best to use the ready made packages on Debian/Ubuntu
and RHEL/CentOS/Scientific Linux.

If you are packaging this software yourself or you want to compile
from source for other reasons, you can follow these instructions.


### Prerequisites

This software is implemented in the Go programming language. See
<https://golang.org/doc/install> for how to install the Go environment.

Dependencies are managed with Go modules (see `go.mod`) and are downloaded
automatically on the first build.


### Compiling

Optionally edit the `Makefile` to set the installation directories.
Then run:

```sh
make
```


### Installation

```sh
make install
```

Other useful targets:

| Target      | Description                       |
|-------------|-----------------------------------|
| `make dist` | Build a source tarball            |
| `make deb`  | Build a Debian package            |
| `make rpm`  | Build RPM packages                |
| `make clean`| Remove build results              |


### Running

There are some OS/distribution specific startup scripts and service
definitions (systemd, upstart, SMF, ...) in the `etc` subdirectory. They may
be useful.

See the installed `zfswatcher(8)` manual page for information on invoking
the zfswatcher process.


## Configuration

Edit the configuration file:

```sh
editor /etc/zfs/zfswatcher.conf
```

Verify the configuration syntax:

```sh
zfswatcher -t
```

Restart the process:

```sh
systemctl restart zfswatcher
```

Some notes:

- See the configuration file comments for information about the configuration
  settings.

- Enclosure LED management is disabled by default. The bundled helper
  `/usr/sbin/zfswatcher-ledctl` (the default `ledctlcmd`) works without root
  through a udev rule that gives the `zfswatcher` group access to the SES
  enclosure. Alternatively the external utility `ledctl` (part of the ledmon
  package) can be used, which needs root, see below.

- Running as root: `ledctl` needs root. Run `systemctl edit zfswatcher` and add

  ```
  [Service]
  User=root
  Group=root
  ```

- The pool state is saved to `/var/lib/zfswatcher/state.json` (`statefile` in
  the `[main]` section, empty disables it). After a restart zfswatcher
  compares against this file and reports only changes since the last run, for
  example a pool which became DEGRADED while the daemon was down. A missing,
  unreadable or incompatible file is logged and everything is reported as new,
  as before. Write errors are logged once and do not stop the daemon.

- Logging to file `/var/log/zfswatcher/zfswatcher.log` and local syslog
  daemon is enabled by default.

- E-mail notifications are disabled by default.

- The embedded web server is disabled by default. When enabled it listens on
  `127.0.0.1:8081` (`bind` in the `[www]` section), meant for a reverse proxy
  on the same host. Use `bind = :8081` to listen on all interfaces. Example
  for nginx under the path `/zfswatcher/` (not tested with every setup):

  ```
  location /zfswatcher/ {
      proxy_pass http://127.0.0.1:8081;
  }
  ```

  together with `rootdir = "/zfswatcher"` in the `[www]` section of
  `zfswatcher.conf`. The proxy passes the login of the built-in HTTP
  authentication on; put TLS on the proxy.

- If you change the default web interface templates, it is best to copy them
  from `/usr/share/zfswatcher/www` to another location and change the
  `templatedir` and `resourcedir` settings in the configuration accordingly.
  This way your local changes will not be overwritten if you upgrade the
  package at a later time.


## Support

If you encounter a bug, please open an issue at the GitHub issue tracker:

<https://github.com/tantefred/zfswatcher/issues>

Reported bugs are fixed on a best effort basis.


## Contributions

Contributions and suggestions are welcome. Feel free to open an issue
or submit a pull request on GitHub. Development happens on the `contrib`
branch.


## Releases and branches

- `contrib` is the development branch and the target of pull requests. Every
  push to it builds the draft pre-release `latest-dev`. Its packages and
  binary carry a development version of the form
  `X.Y.Z+gitYYYYMMDDhhmmss.<sha>` (e.g. `0.5.0+git20261122153012.a6cae3c`, Debian
  revision `-1`), which sorts after the release `X.Y.Z` and before the next one.
- `master` contains released states only. It is only ever fast-forwarded to
  `contrib`.
- Hotfix for a released version: branch from `master`, open a pull request
  against `master`, release from there, then merge `master` back into
  `contrib`.

Release steps for the maintainer:

1. On a branch from `contrib`, set the new version in `version.go`, add a
   `X.Y.Z-1` entry (not `UNRELEASED`) at the top of `debian/changelog` and a
   `version X.Y.Z released YYYY-MM-DD` section at the top of `NEWS`. The RPM
   takes its version from `version.go`, so `zfswatcher.spec` only needs a
   change if the packaging itself changed. Open a pull request against
   `contrib` and merge it.
2. Fast-forward `master` to `contrib`:
   `git switch master && git merge --ff-only contrib && git push origin master`.
3. Tag the commit on `master` without a `v` prefix and push the tag:
   `git tag X.Y.Z && git push origin X.Y.Z`.
4. The `Release` workflow checks that the tag is on `master`, that it matches
   `version.go` and `debian/changelog`, builds the tarball, `.deb` and `.rpm`
   packages and creates a draft release with the matching section of `NEWS`
   as release notes. It never overwrites an existing release.
5. Review the draft on GitHub and publish it by hand.


## Security

Please report vulnerabilities privately, see [SECURITY.md](SECURITY.md).

## Credits

- Janne Snabb <snabb@epipe.com> - original author
  ([damicon/zfswatcher](https://github.com/damicon/zfswatcher),
  [original project site](http://zfswatcher.damicon.fi/))
- Rouben Tchakhmakhtchian - maintainer of the
  [rouben/zfswatcher](https://github.com/rouben/zfswatcher) fork
- Further contributions by slacker87, Paul Koelle, Der-Jan, kopernikus,
  StripeBanana and others. See the git history for details.


## License

Copyright © 2012-2013 Damicon Kraa Oy

Zfswatcher is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zfswatcher is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with zfswatcher. If not, see <http://www.gnu.org/licenses/>.
