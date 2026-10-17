#!/bin/sh
set -eu

version=v0.45.6
asset=kindling-cli-mac-apple-silicon
sha256=3de02c4ed420e3ebb42f2a88a8c911dbcf179e41fbedaa77e901dddef8d362a8
repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
target="$repo_root/third_party/kindling/darwin-arm64/kindling-cli"
license="$repo_root/third_party/kindling/LICENSE"
tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT HUP INT TERM
mkdir -p "$(dirname "$target")"
curl -fL --retry 3 "https://github.com/ciscoriordan/kindling/releases/download/$version/$asset" -o "$tmp"
actual=$(shasum -a 256 "$tmp" | awk '{print $1}')
if [ "$actual" != "$sha256" ]; then
  echo "Kindling checksum mismatch: $actual" >&2
  exit 1
fi
install -m 755 "$tmp" "$target"
curl -fsSL "https://raw.githubusercontent.com/ciscoriordan/kindling/$version/LICENSE" -o "$license"
printf 'Fetched Kindling %s; sha256=%s\n' "$version" "$actual"
