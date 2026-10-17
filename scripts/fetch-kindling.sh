#!/bin/sh
set -eu
repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
. "$repo_root/scripts/kindling-pins.sh"
goos=${1:-$(go env GOOS)}
goarch=${2:-$(go env GOARCH)}
asset=$(kindling_asset "$goos" "$goarch")
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
