Name:		zfswatcher
Version:	%{version}
Release:	1%{?dist}
Summary:	ZFS pool monitoring and notification daemon

Group:		Applications/System
License:	GPLv3+
Vendor:		Damicon Kraa Oy <http://www.damicon.fi/>
Packager:	Janne Snabb <snabb@epipe.com>
URL:		http://zfswatcher.damicon.fi/
Source0:	%{name}-%{version}.tar.gz
ExclusiveArch:	x86_64
BuildRoot:	%{_tmppath}/%{name}-%{version}-%{release}-root-%(%{__id_u} -n)

%define		debug_package %{nil}
%define		unit %{name}.service
%define		user %{name}
%define		group %{name}


#BuildRequires:	# Go 1.0.3
Requires:		zfs
Requires(pre):		/usr/sbin/useradd, /usr/sbin/groupadd, /usr/bin/getent
Requires(postun):	/usr/sbin/userdel, /usr/sbin/groupdel
Requires(post):		systemd
Requires(preun):	systemd
Requires(postun):	systemd

%description
Zfswatcher is ZFS pool monitoring and notification daemon
with the following main features:
 * Periodically inspects the zpool status.
 * Sends configurable notifications on status changes.
 * Controls the disk enclosure LEDs.
 * Web interface for displaying status and logs.

%prep
%setup -q


%build
make


%install
rm -rf $RPM_BUILD_ROOT
make install DESTDIR=$RPM_BUILD_ROOT

%__mkdir_p ${RPM_BUILD_ROOT}%{_unitdir}
%__install -p -m 755 etc/%{unit} ${RPM_BUILD_ROOT}%{_unitdir}/%{unit}

%__mkdir_p -m 755 ${RPM_BUILD_ROOT}%{_sysconfdir}/logrotate.d
%__install -p -m 644 etc/logrotate.conf ${RPM_BUILD_ROOT}%{_sysconfdir}/logrotate.d/%{name}

%__mkdir_p -m 755 ${RPM_BUILD_ROOT}%{_var}/log/%{name}


%clean
rm -rf $RPM_BUILD_ROOT


%files
%defattr(-,root,root,-)
%doc README.md COPYING NEWS
%{_sbindir}/*
%{_mandir}/man8/*
%{_datadir}/%{name}/
%config(noreplace) %{_unitdir}/%{unit}
%config(noreplace) %{_sysconfdir}/zfs/*.conf
%config(noreplace) %{_sysconfdir}/logrotate.d/*
%dir %attr(0755, %{name}, %{name}) %{_var}/log/%{name}


%pre
/usr/bin/getent group %{group} || /usr/sbin/groupadd -r %{group}
/usr/bin/getent passwd %{user} || /usr/sbin/useradd -r -d %{_sbindir} -s /sbin/nologin -g %{group} %{user}


%post
%systemd_post %{unit}


%preun
%systemd_preun %{unit}


%postun
%systemd_postun %{unit}
/usr/sbin/userdel %{name}
/usr/sbin/groupdel %{name}
