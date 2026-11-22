#
# Makefile - ZFS pool monitoring and notification daemon
#
# Copyright © 2012-2013 Damicon Kraa Oy
#
# This file is part of zfswatcher.
#
# Zfswatcher is free software: you can redistribute it and/or modify
# it under the terms of the GNU General Public License as published by
# the Free Software Foundation, either version 3 of the License, or
# (at your option) any later version.
#
# Zfswatcher is distributed in the hope that it will be useful,
# but WITHOUT ANY WARRANTY; without even the implied warranty of
# MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
# GNU General Public License for more details.
#
# You should have received a copy of the GNU General Public License
# along with zfswatcher. If not, see <http://www.gnu.org/licenses/>.
#

# Shell:
SHELL	= /bin/sh

# Go tool:
GO	= $(shell which go)

# Installation directories:
prefix		= /usr
exec_prefix	= $(prefix)
bindir		= $(exec_prefix)/bin
sbindir		= $(exec_prefix)/sbin
datarootdir	= $(prefix)/share
datadir		= $(datarootdir)
sysconfdir	= /etc
docdir		= $(datarootdir)/doc/zfswatcher
mandir		= $(datarootdir)/man
man1dir		= $(mandir)/man1
man5dir		= $(mandir)/man5
man8dir		= $(mandir)/man8

VERSION		= $(shell fgrep VERSION version.go | cut -d\" -f2)

# Version of the built binary and the packages. Equal to VERSION for releases;
# CI overrides it for development builds (e.g. 0.5.0+git20261122153012.a6cae3c).
PKGVERSION	?= $(VERSION)

# Git revision archived by "make dist". Defaults to the current commit so that
# CI builds the code under test; use REF=$(VERSION) to archive the release tag.
REF		?= HEAD

# On Linux build a static, pure Go binary. The C parts of the Go runtime that
# cgo would add are compiled with the CPU flags of the build machine; the CI
# runner's compiler emitted AVX instructions, which crash with "Illegal
# instruction" on virtual CPUs without AVX (e.g. QEMU's default CPU type).
# Only the FreeBSD port needs cgo.
ifeq ($(shell $(GO) env GOOS),linux)
export CGO_ENABLED = 0
endif

# Rules:
all: zfswatcher

zfswatcher:
	$(GO) build -ldflags "-X main.buildVersion=$(PKGVERSION)" -o $@

clean: 
	$(GO) clean
	$(RM) zfswatcher zfswatcher-*.tar.gz zfswatcher-*.rpm		\
		zfswatcher_*.deb zfswatcher_*.changes

install: zfswatcher
	install -d $(DESTDIR)$(sbindir) $(DESTDIR)$(sysconfdir)/zfs	\
		$(DESTDIR)$(datadir)/zfswatcher				\
		$(DESTDIR)$(man8dir)
	install -c zfswatcher $(DESTDIR)$(sbindir)/zfswatcher
	test -e $(DESTDIR)$(sysconfdir)/zfs/zfswatcher.conf		\
		|| install -c -m 644 etc/zfswatcher.conf		\
			$(DESTDIR)$(sysconfdir)/zfs/zfswatcher.conf
	install -c -m 644 doc/zfswatcher.8				\
		$(DESTDIR)$(man8dir)/zfswatcher.8
	install -c -m 644 doc/zfswatcher-ledctl.8			\
		$(DESTDIR)$(man8dir)/zfswatcher-ledctl.8
	cp -R www $(DESTDIR)$(datadir)/zfswatcher/

# Make tarball:
dist:
	version=$(PKGVERSION) &&						\
	git archive --prefix=zfswatcher-$${version}/			\
		-o zfswatcher-$${version}.tar.gz $(REF)

# Make a new Debian package version:
newdebversion:
	version=$(VERSION) &&						\
	dch --newversion $${version}-1					\
		--upstream						\
		--distribution unstable 				\
		"New version $${version}"

# Make Debian package:
deb:
	version=$(PKGVERSION) &&						\
	dpkg-buildpackage -b -uc -tc &&					\
	mv ../zfswatcher_$${version}-*.deb 				\
		../zfswatcher_$${version}-*.changes 			\
		.

# Make RPM package:
rpm:	dist
	version=$(PKGVERSION) &&						\
	rpmbuild=$(shell mktemp -d "/tmp/zfswatcher-rpmbuild-XXXXXXXX");\
	mkdir -p $$rpmbuild/TMP &&					\
	mkdir -p $$rpmbuild/BUILD &&					\
	mkdir -p $$rpmbuild/RPMS &&					\
	mkdir -p $$rpmbuild/SRPMS &&					\
	mkdir -p $$rpmbuild/SPECS &&					\
	cp zfswatcher.spec $$rpmbuild/SPECS/ &&				\
	mkdir -p $$rpmbuild/SOURCES &&					\
	cp zfswatcher-$${version}.tar.gz $$rpmbuild/SOURCES/ &&		\
	rpmbuild -ba							\
		--define "_topdir $$rpmbuild"				\
		--define "_tmppath $$rpmbuild/TMP"			\
		--define "version $${version}"				\
		zfswatcher.spec &&					\
	cp $$rpmbuild/RPMS/*/* . &&					\
	cp $$rpmbuild/SRPMS/* . &&					\
	rm -r $$rpmbuild &&						\
	echo "RPM build finished"

# eof
