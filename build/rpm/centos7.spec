# build with the following command:
# rpmbuild -bb
%define debug_package %{nil}

Name:       dr-tools
Version:    %{getenv:VERSION}
Release:    1%{?dist}
Summary:    Tools for managing data in the Donders Repository.
License:    FIXME
URL: https://github.com/Donders-Institute/%{name}
Source0: https://github.com/Donders-Institute/%{name}/archive/%{version}.tar.gz

BuildArch: x86_64

# defin the GOPATH that is created later within the extracted source code.
%define gopath %{_tmppath}/go.rpmbuild-%{name}-%{version}

%description
CLI tools for interacting various services managed by the TG.

%prep
%setup -q

%build
# create GOPATH structure within the source code
mkdir -p %{gopath}
# copy entire directory into gopath, this duplicate the source code
GOPATH=%{gopath} make

%install
mkdir -p %{buildroot}/%{_bindir}
mkdir -p %{buildroot}/%{_sbindir}
#mkdir -p %{buildroot}/%{_sysconfdir}/bash_completion.d
## install files for client tools
install -m 755 %{gopath}/bin/repoadm %{buildroot}/%{_sbindir}/repoadm
install -m 755 %{gopath}/bin/repocli %{buildroot}/%{_bindir}/repocli

%files
%{_sbindir}/repoadm
%{_bindir}/repocli

%clean
chmod -R +w %{gopath}
rm -rf %{gopath}
rm -f %{_topdir}/SOURCES/%{version}.tar.gz
rm -rf $RPM_BUILD_ROOT

%changelog
* Thu Jan 27 2022 Hong Lee <h.lee@donders.ru.nl> - 0.1
- first rpmbuild implementation
