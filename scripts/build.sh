#!/bin/sh
set -eu
. "$(dirname "$0")/env.sh"
ATTUNE_VERSION=${ATTUNE_VERSION:-$(node -p 'require("./frontend/package.json").version')}
node -e 'if (!/^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-(alpha|beta|rc)(\.(0|[1-9][0-9]*))?)?$/.test(process.argv[1])) process.exit(2)' "$ATTUNE_VERSION" || {
  echo "ATTUNE_VERSION must be MAJOR.MINOR.PATCH with an optional alpha, beta, or rc suffix" >&2
  exit 2
}
ARCH=$(go env GOARCH)
if [ "$(go env GOOS)" != darwin ]; then
  echo "The first desktop build targets macOS." >&2
  exit 1
fi
if [ ! -x "third_party/kindling/darwin-$ARCH/kindling-cli" ]; then
  echo "Fetch the pinned Kindling helper first; see README.md." >&2
  exit 1
fi
if [ ! -x "$GOBIN/wails3" ]; then
  if [ "$(go env GOOS)" = linux ]; then
    go install -tags gtk3 github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0
  else
    go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0
  fi
fi
MTP_ARCH=$ARCH
[ "$MTP_ARCH" != amd64 ] || MTP_ARCH=x86_64
MTP_PREFIX=${ATTUNE_MTP_PREFIX:-$APP_ROOT/.cache/macos-mtp/$MTP_ARCH}
scripts/build-macos-mtp.sh "$MTP_ARCH"
export PKG_CONFIG_PATH="$MTP_PREFIX/lib/pkgconfig${PKG_CONFIG_PATH:+:$PKG_CONFIG_PATH}"
export EXTRA_TAGS=libmtp
export EXTRA_LDFLAGS="-X attune/internal/app.appVersion=$ATTUNE_VERSION -X attune/internal/app.updateEnabled=true"
wails3 package
APP="build/bin/Attune.app"
mkdir -p "$APP/Contents/Resources/licenses"
cp "third_party/kindling/darwin-$ARCH/kindling-cli" "$APP/Contents/Resources/kindling-cli"
cp LICENSE "$APP/Contents/Resources/licenses/Attune-LICENSE.txt"
cp THIRD_PARTY_NOTICES.md THIRD_PARTY_LICENSES.txt "$APP/Contents/Resources/licenses/"
cp third_party/kindling/LICENSE "$APP/Contents/Resources/licenses/Kindling-LICENSE.txt"
cp third_party/kindling/THIRD_PARTY_LICENSES.txt "$APP/Contents/Resources/licenses/Kindling-THIRD_PARTY_LICENSES.txt"
scripts/bundle-macos-mtp.sh "$APP" "$MTP_PREFIX"
/usr/libexec/PlistBuddy -c "Set :LSMinimumSystemVersion 13.0" "$APP/Contents/Info.plist"
# Local distribution only. Developer ID signing/notarization is a separate release step.
codesign --force --sign - "$APP/Contents/Resources/kindling-cli"
codesign --force --deep --sign - "$APP"
codesign --verify --deep --strict "$APP"
echo "Built: $APP_ROOT/$APP"
