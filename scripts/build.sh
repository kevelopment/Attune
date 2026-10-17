#!/bin/sh
set -eu
. "$(dirname "$0")/env.sh"
ARCH=$(go env GOARCH)
if [ "$(go env GOOS)" != darwin ]; then
  echo "The first desktop build targets macOS." >&2
  exit 1
fi
if [ ! -x "third_party/kindling/darwin-$ARCH/kindling-cli" ]; then
  echo "Fetch the pinned Kindling helper first; see README.md." >&2
  exit 1
fi
if [ ! -x "$GOBIN/wails" ]; then
  go install github.com/wailsapp/wails/v2/cmd/wails@v2.15.0
fi
wails build -clean
APP="build/bin/Attune.app"
mkdir -p "$APP/Contents/Resources/licenses"
cp "third_party/kindling/darwin-$ARCH/kindling-cli" "$APP/Contents/Resources/kindling-cli"
cp third_party/kindling/LICENSE* "$APP/Contents/Resources/licenses/"
/usr/libexec/PlistBuddy -c "Set :LSMinimumSystemVersion 13.0" "$APP/Contents/Info.plist"
# Local distribution only. Developer ID signing/notarization is a separate release step.
codesign --force --sign - "$APP/Contents/Resources/kindling-cli"
codesign --force --deep --sign - "$APP"
codesign --verify --deep --strict "$APP"
echo "Built: $APP_ROOT/$APP"
