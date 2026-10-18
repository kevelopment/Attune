#!/bin/sh
set -eu

app=${1:?usage: scripts/build-macos-dmg.sh APP_BUNDLE GOARCH}
goarch=${2:?usage: scripts/build-macos-dmg.sh APP_BUNDLE GOARCH}

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
deps="$root/build/dmg-python"
background="$root/build/darwin/dmg-background.png"
mkdir -p "$root/build/darwin" "$root/dist"
rm -rf "$deps"
python3 -m pip install --disable-pip-version-check --target "$deps" -r "$root/scripts/requirements-macos-dmg.txt"
PYTHONPATH="$deps" python3 "$root/scripts/macos-dmg-background.py" "$background"
ATTUNE_APP="$app" ATTUNE_DMG_BACKGROUND="$background" PYTHONPATH="$deps" \
  python3 -m dmgbuild --no-hidpi -s "$root/scripts/macos-dmg-settings.py" \
  "Attune" "$root/dist/Attune-macos-$goarch.dmg"
hdiutil verify "$root/dist/Attune-macos-$goarch.dmg"
printf 'Created macOS drag-to-Applications DMG for %s\n' "$goarch"
