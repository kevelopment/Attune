#!/bin/sh
set -eu
. "$(dirname "$0")/env.sh"
export ATTUNE_DEV_PORT="${ATTUNE_DEV_PORT:-5183}"
export EBOOK_LIBRARY_HOME="${ATTUNE_DEV_LIBRARY_HOME:-$APP_ROOT/.cache/dev-library}"
export EBOOK_LIBRARY_CONVERTER="$APP_ROOT/third_party/kindling/$(go env GOOS)-$(go env GOARCH)/kindling-cli"
status=0
node scripts/dev-process.mjs status "$GOBIN/wails" || status=$?
case "$status" in 0) exit 0 ;; 1) ;; *) exit "$status" ;; esac
if [ ! -x "$GOBIN/wails" ]; then
  go install github.com/wailsapp/wails/v2/cmd/wails@v2.15.0
fi
if [ ! -x "$EBOOK_LIBRARY_CONVERTER" ]; then ./scripts/fetch-kindling.sh; fi
(cd frontend && npm ci)
mkdir -p frontend/dist "$EBOOK_LIBRARY_HOME"
# Keep Go embedding valid on a fresh clone; dev assets come from Vite.
: > frontend/dist/gitkeep
exec node scripts/dev-process.mjs start "$GOBIN/wails"
