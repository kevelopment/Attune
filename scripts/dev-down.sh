#!/bin/sh
set -eu
. "$(dirname "$0")/env.sh"
exec node scripts/dev-process.mjs stop "$GOBIN/wails3"
