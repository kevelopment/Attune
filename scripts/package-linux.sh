#!/bin/sh
set -eu
. "$(dirname -- "$0")/env.sh"

version=${1:?usage: scripts/package-linux.sh VERSION ARCH STAGE}
arch=${2:?usage: scripts/package-linux.sh VERSION ARCH STAGE}
stage=${3:?usage: scripts/package-linux.sh VERSION ARCH STAGE}
case "$arch" in
  amd64) deb_arch=amd64; rpm_arch=x86_64 ;;
  arm64) deb_arch=arm64; rpm_arch=aarch64 ;;
  *) echo "unsupported Linux package architecture: $arch" >&2; exit 2 ;;
esac

command -v dpkg-deb >/dev/null 2>&1 || { echo "dpkg-deb is required to build the DEB package." >&2; exit 1; }
command -v rpmbuild >/dev/null 2>&1 || { echo "rpmbuild is required to build the RPM package." >&2; exit 1; }

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT HUP INT TERM
root="$work/root"
install_dir="$root/usr/lib/attune"
mkdir -p "$install_dir" "$root/usr/bin" "$root/usr/share/applications" "$root/usr/share/icons/hicolor/scalable/apps" "$root/usr/share/doc/attune/licenses"
cp "$stage/Attune" "$stage/kindling-cli" "$install_dir/"
chmod 755 "$install_dir/Attune" "$install_dir/kindling-cli"
cp "$stage"/licenses/* "$root/usr/share/doc/attune/licenses/"
cp frontend/public/attune.svg "$root/usr/share/icons/hicolor/scalable/apps/attune.svg"
cat > "$root/usr/bin/attune" <<'EOF'
#!/bin/sh
exec /usr/lib/attune/Attune "$@"
EOF
chmod 755 "$root/usr/bin/attune"
cat > "$root/usr/share/applications/attune.desktop" <<'EOF'
[Desktop Entry]
Name=Attune
Comment=Manage and read your ebook library
Exec=attune
Icon=attune
Terminal=false
Type=Application
Categories=Office;Utility;
EOF

deb_root="$work/deb"
deb_version=$(printf '%s' "$version" | sed 's/-/~/')
mkdir -p "$deb_root"
cp -a "$root"/. "$deb_root/"
mkdir -p "$deb_root/DEBIAN"
cat > "$deb_root/DEBIAN/control" <<EOF
Package: attune
Version: $deb_version
Section: utils
Priority: optional
Architecture: $deb_arch
Maintainer: kevelopment
Depends: libc6, libgtk-3-0t64 | libgtk-3-0, libwebkit2gtk-4.1-0, libmtp9t64 | libmtp9
Description: Attune ebook library manager
 Desktop library and reader with experimental USB Kindle management.
EOF
dpkg-deb --build --root-owner-group "$deb_root" "dist/Attune-linux-$arch-$version.deb"

rpm_top="$work/rpmbuild"
mkdir -p "$rpm_top/BUILD" "$rpm_top/BUILDROOT" "$rpm_top/RPMS" "$rpm_top/SOURCES" "$rpm_top/SPECS" "$rpm_top/SRPMS"
tar -C "$root" -czf "$rpm_top/SOURCES/attune-files.tar.gz" .
rpm_version=$(printf '%s' "$version" | sed 's/-/~/')
cat > "$rpm_top/SPECS/attune.spec" <<EOF
Name:           attune
Version:        $rpm_version
Release:        1%{?dist}
Summary:        Attune ebook library manager
License:        MIT
BuildArch:      $rpm_arch
Source0:        attune-files.tar.gz
Requires:       glibc, gtk3, webkit2gtk4.1, libmtp

%description
Desktop library and reader with experimental USB Kindle management.

%prep
%setup -q -c -T
tar -xzf %{SOURCE0}

%install
install -d %{buildroot}
cp -a . %{buildroot}/

%files
/usr/bin/attune
/usr/lib/attune/Attune
/usr/lib/attune/kindling-cli
/usr/share/applications/attune.desktop
/usr/share/icons/hicolor/scalable/apps/attune.svg
/usr/share/doc/attune/licenses/*
EOF
rpmbuild --define "_topdir $rpm_top" --target "$rpm_arch" -bb "$rpm_top/SPECS/attune.spec"
cp "$rpm_top/RPMS/$rpm_arch/attune-"*.rpm "dist/Attune-linux-$arch-$version.rpm"
printf 'Created DEB and RPM packages for Linux %s (%s)\n' "$arch" "$version"
