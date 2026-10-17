#!/bin/sh
# Keep build tools and caches scoped to this checkout.
APP_ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
export GOPATH="${GOPATH:-$APP_ROOT/.cache/go}"
export GOMODCACHE="${GOMODCACHE:-$GOPATH/pkg/mod}"
export GOCACHE="${GOCACHE:-$APP_ROOT/.cache/go-build}"
export GOBIN="${GOBIN:-$APP_ROOT/.cache/bin}"
export npm_config_cache="${npm_config_cache:-$APP_ROOT/.cache/npm}"
if [ "$(go env GOOS)" = darwin ]; then
  # Keep the native deployment target aligned with the Go runtime.
  export CGO_CFLAGS="${CGO_CFLAGS:-} -mmacosx-version-min=13.0"
  export CGO_LDFLAGS="${CGO_LDFLAGS:-} -mmacosx-version-min=13.0"
fi
export PATH="$GOBIN:$PATH"
mkdir -p "$GOBIN"
cd "$APP_ROOT"
