#!/bin/sh
set -eu
. "$(dirname "$0")/env.sh"
if [ ! -x "$GOBIN/wails" ]; then
  go install github.com/wailsapp/wails/v2/cmd/wails@v2.15.0
fi
exec wails dev
