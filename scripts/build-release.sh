#!/bin/sh
set -eu
. "$(dirname -- "$0")/env.sh"
VERSION=${1:?usage: scripts/build-release.sh VERSION [webkit-tag]}
TAG=${2:-}
node -e 'if (!/^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/.test(process.argv[1])) process.exit(2)' "$VERSION" || {
  echo "product version must be a numeric MAJOR.MINOR.PATCH" >&2
  exit 2
}
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
backup=$(mktemp)
cp wails.json "$backup"
restore_config() { cp "$backup" wails.json; rm -f "$backup"; }
trap restore_config EXIT HUP INT TERM
node -e 'const fs=require("fs"); const p="wails.json"; const j=JSON.parse(fs.readFileSync(p,"utf8")); j.info.productVersion=process.argv[1]; fs.writeFileSync(p, JSON.stringify(j,null,2)+"\n")' "$VERSION"
if [ -n "$TAG" ]; then
  wails build -clean -platform "$goos/$goarch" -tags "$TAG"
else
  wails build -clean -platform "$goos/$goarch"
fi
stage="build/release/Attune-$goos-$goarch"
mkdir -p "$stage/licenses" dist
cp LICENSE "$stage/licenses/Attune-LICENSE.txt"
cp THIRD_PARTY_NOTICES.md "$stage/licenses/"
cp third_party/kindling/LICENSE "$stage/licenses/Kindling-LICENSE.txt"
if [ "$goos" = darwin ]; then
  app=build/bin/Attune.app
  mkdir -p "$app/Contents/Resources/licenses"
  cp "$helper" "$app/Contents/Resources/kindling-cli"
  cp LICENSE "$app/Contents/Resources/licenses/Attune-LICENSE.txt"
  cp THIRD_PARTY_NOTICES.md "$app/Contents/Resources/licenses/"
  cp third_party/kindling/LICENSE "$app/Contents/Resources/licenses/Kindling-LICENSE.txt"
  /usr/libexec/PlistBuddy -c 'Set :LSMinimumSystemVersion 13.0' "$app/Contents/Info.plist"
  codesign --force --sign - "$app/Contents/Resources/kindling-cli"
  codesign --force --deep --sign - "$app"
  codesign --verify --deep --strict "$app"
  ditto -c -k --sequesterRsrc --keepParent "$app" "dist/Attune-macos-$goarch.zip"
elif [ "$goos" = linux ]; then
  cp build/bin/Attune "$stage/Attune"
  cp "$helper" "$stage/kindling-cli"
  chmod 755 "$stage/Attune" "$stage/kindling-cli"
  cat > "$stage/INSTALL.txt" <<'EOF'
Attune Linux amd64 (experimental; library features only)

Run ./Attune. This build requires GTK 3 and WebKit2GTK 4.1 runtime libraries.
Kindle USB discovery, transfer, and eject are available only in the macOS build.
EOF
  tar -czf "dist/Attune-linux-$goarch.tar.gz" -C build/release "Attune-$goos-$goarch"
else
  echo "scripts/build-release.sh supports macOS and Linux; use scripts/build-release.ps1 on Windows" >&2
  exit 1
fi
printf 'Created dist package for %s/%s (version %s)\n' "$goos" "$goarch" "$VERSION"
