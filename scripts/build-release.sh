#!/bin/sh
set -eu
. "$(dirname -- "$0")/env.sh"
VERSION=${1:?usage: scripts/build-release.sh VERSION [webkit-tag]}
TAG=${2:-}
node -e 'if (!/^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-(alpha|beta|rc)(\.(0|[1-9][0-9]*))?)?$/.test(process.argv[1])) process.exit(2)' "$VERSION" || {
  echo "version must be MAJOR.MINOR.PATCH with an optional alpha, beta, or rc suffix" >&2
  exit 2
}
PRODUCT_VERSION=${VERSION%%-*}
goos=$(go env GOOS)
goarch=$(go env GOARCH)
helper=third_party/kindling/$goos-$goarch/kindling-cli
[ "$goos" != windows ] || helper="$helper.exe"
if [ ! -x "$helper" ]; then
  echo "missing pinned helper: $helper (run scripts/fetch-kindling.sh)" >&2
  exit 1
fi
if [ ! -x "$GOBIN/wails" ]; then
  go install github.com/wailsapp/wails/v2/cmd/wails@v2.15.0
fi
BUILD_TAGS=$TAG
if [ "$goos" = darwin ]; then
  mtp_arch=$goarch
  [ "$mtp_arch" != amd64 ] || mtp_arch=x86_64
  MTP_PREFIX=${ATTUNE_MTP_PREFIX:-$APP_ROOT/.cache/macos-mtp/$mtp_arch}
  scripts/build-macos-mtp.sh "$mtp_arch"
  export PKG_CONFIG_PATH="$MTP_PREFIX/lib/pkgconfig${PKG_CONFIG_PATH:+:$PKG_CONFIG_PATH}"
  BUILD_TAGS=${BUILD_TAGS:+$BUILD_TAGS,}libmtp
elif [ "$goos" = linux ]; then
  if ! command -v pkg-config >/dev/null 2>&1 || ! pkg-config --exists libmtp; then
    echo "Linux release builds require libmtp development files (for example, libmtp-dev and pkg-config)." >&2
    exit 1
  fi
  BUILD_TAGS=${BUILD_TAGS:+$BUILD_TAGS,}libmtp
fi
backup=$(mktemp)
cp wails.json "$backup"
restore_config() { cp "$backup" wails.json; rm -f "$backup"; }
trap restore_config EXIT HUP INT TERM
node -e 'const fs=require("fs"); const p="wails.json"; const j=JSON.parse(fs.readFileSync(p,"utf8")); j.info.productVersion=process.argv[1]; fs.writeFileSync(p, JSON.stringify(j,null,2)+"\n")' "$PRODUCT_VERSION"
if [ -n "$BUILD_TAGS" ]; then
  wails build -clean -platform "$goos/$goarch" -tags "$BUILD_TAGS" -ldflags "-X attune/internal/app.appVersion=$VERSION -X attune/internal/app.updateEnabled=true"
else
  wails build -clean -platform "$goos/$goarch" -ldflags "-X attune/internal/app.appVersion=$VERSION -X attune/internal/app.updateEnabled=true"
fi
stage="build/release/Attune-$goos-$goarch"
mkdir -p "$stage/licenses" dist
cp LICENSE "$stage/licenses/Attune-LICENSE.txt"
cp THIRD_PARTY_NOTICES.md "$stage/licenses/"
cp THIRD_PARTY_LICENSES.txt "$stage/licenses/"
cp third_party/kindling/THIRD_PARTY_LICENSES.txt "$stage/licenses/Kindling-THIRD_PARTY_LICENSES.txt"
cp third_party/kindling/LICENSE "$stage/licenses/Kindling-LICENSE.txt"
if [ "$goos" = darwin ]; then
  app=build/bin/Attune.app
  mkdir -p "$app/Contents/Resources/licenses"
  cp "$helper" "$app/Contents/Resources/kindling-cli"
  cp LICENSE "$app/Contents/Resources/licenses/Attune-LICENSE.txt"
  cp THIRD_PARTY_NOTICES.md "$app/Contents/Resources/licenses/"
  cp THIRD_PARTY_LICENSES.txt "$app/Contents/Resources/licenses/"
  cp third_party/kindling/THIRD_PARTY_LICENSES.txt "$app/Contents/Resources/licenses/Kindling-THIRD_PARTY_LICENSES.txt"
  cp third_party/kindling/LICENSE "$app/Contents/Resources/licenses/Kindling-LICENSE.txt"
  scripts/bundle-macos-mtp.sh "$app" "$MTP_PREFIX"
  cp "$MTP_PREFIX"/share/licenses/libmtp/* "$stage/licenses/"
  cp "$MTP_PREFIX"/share/licenses/libusb/* "$stage/licenses/"
  /usr/libexec/PlistBuddy -c 'Set :LSMinimumSystemVersion 13.0' "$app/Contents/Info.plist"
  codesign --force --sign - "$app/Contents/Resources/kindling-cli"
  codesign --force --deep --sign - "$app"
  codesign --verify --deep --strict "$app"
  update_archive="dist/Attune-macos-$goarch.zip"
  ditto -c -k --sequesterRsrc --keepParent "$app" "$update_archive"
  python3 - "$update_archive" <<'PY'
import stat
import sys
import zipfile

with zipfile.ZipFile(sys.argv[1]) as archive:
    for entry in archive.infolist():
        file_type = stat.S_IFMT(entry.external_attr >> 16)
        if file_type not in (0, stat.S_IFREG, stat.S_IFDIR):
            raise SystemExit(f"update archive contains a non-regular entry: {entry.filename}")
PY
  sh scripts/build-macos-dmg.sh "$app" "$goarch"
elif [ "$goos" = linux ]; then
  cp build/bin/Attune "$stage/Attune"
  cp "$helper" "$stage/kindling-cli"
  chmod 755 "$stage/Attune" "$stage/kindling-cli"
  cat > "$stage/INSTALL.txt" <<'EOF'
Attune Linux amd64 (experimental)

Run ./Attune. This build requires GTK 3, WebKit2GTK 4.1, and libmtp runtime libraries.
MTP access may require a udev rule granting your account permission to the reader. Close other MTP clients if a reader is busy. USB mass-storage discovery, transfer, and eject need lsblk, udisksctl, and UDisks/polkit permissions. Hardware validation is limited; ensure storage readers are mounted before transferring.
EOF
  tar -czf "dist/Attune-linux-$goarch.tar.gz" -C build/release "Attune-$goos-$goarch"
else
  echo "scripts/build-release.sh supports macOS and Linux; use scripts/build-release.ps1 on Windows" >&2
  exit 1
fi
printf 'Created dist package for %s/%s (version %s)\n' "$goos" "$goarch" "$VERSION"
