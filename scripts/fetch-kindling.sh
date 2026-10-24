#!/bin/sh
set -eu
repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
. "$repo_root/scripts/kindling-pins.sh"
goos=${1:-$(go env GOOS)}
goarch=${2:-$(go env GOARCH)}
asset=$(kindling_asset "$goos" "$goarch")
if [ "$goos/$goarch" = linux/arm64 ]; then
  command -v cargo >/dev/null 2>&1 || {
    echo "Building Kindling for Linux arm64 requires Rust and Cargo 1.85 or newer." >&2
    exit 1
  }
  target="$repo_root/third_party/kindling/$goos-$goarch/kindling-cli"
  mkdir -p "$(dirname "$target")"
  install_root=$(mktemp -d)
  trap 'rm -rf "$install_root"' EXIT HUP INT TERM
  cargo install --git https://github.com/ciscoriordan/kindling.git --tag v0.45.6 --locked --root "$install_root" kindling-mobi
  install -m 755 "$install_root/bin/kindling-cli" "$target"
  printf 'Built Kindling v0.45.6 from source for %s/%s\n' "$goos" "$goarch"
  exit 0
fi
expected=$(kindling_sha256 "$goos" "$goarch")
filename=kindling-cli
[ "$goos" != windows ] || filename=kindling-cli.exe
target="$repo_root/third_party/kindling/$goos-$goarch/$filename"
tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT HUP INT TERM
mkdir -p "$(dirname "$target")"
curl -fL --retry 3 "https://github.com/ciscoriordan/kindling/releases/download/v0.45.6/$asset" -o "$tmp"
actual=$(shasum -a 256 "$tmp" | awk '{print $1}')
if [ "$actual" != "$expected" ]; then
  echo "Kindling checksum mismatch for $goos/$goarch: $actual" >&2
  exit 1
fi
install -m 755 "$tmp" "$target"
if [ ! -f "$repo_root/third_party/kindling/LICENSE" ]; then
  curl -fsSL https://raw.githubusercontent.com/ciscoriordan/kindling/v0.45.6/LICENSE -o "$repo_root/third_party/kindling/LICENSE"
fi
printf 'Fetched Kindling v0.45.6 for %s/%s; sha256=%s\n' "$goos" "$goarch" "$actual"
