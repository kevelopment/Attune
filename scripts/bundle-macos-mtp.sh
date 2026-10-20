#!/bin/sh
set -eu
APP=${1:?usage: bundle-macos-mtp.sh APP_PATH PREFIX}
PREFIX=${2:?usage: bundle-macos-mtp.sh APP_PATH PREFIX}
FRAMEWORKS="$APP/Contents/Frameworks"
EXECUTABLE="$APP/Contents/MacOS/Attune"
mkdir -p "$FRAMEWORKS" "$APP/Contents/Resources/licenses"
[ -f "$EXECUTABLE" ] || { echo "missing app executable: $EXECUTABLE" >&2; exit 1; }

for source in "$PREFIX"/lib/libmtp*.dylib "$PREFIX"/lib/libusb-1.0*.dylib; do
  [ -f "$source" ] || continue
  # The updater intentionally rejects archive symlinks. Dereference the
  # versioned-library links so the app's update ZIP contains regular files.
  cp -L "$source" "$FRAMEWORKS/$(basename "$source")"
done
[ -f "$FRAMEWORKS/libmtp.9.dylib" ] || { echo "bundled libmtp dylib is missing" >&2; exit 1; }
[ -f "$FRAMEWORKS/libusb-1.0.0.dylib" ] || { echo "bundled libusb dylib is missing" >&2; exit 1; }

# The app links libmtp; libmtp links libusb. Rewrite both levels to use only
# app-relative paths so no Homebrew or build-cache paths escape into the app.
for library in "$FRAMEWORKS"/*.dylib; do
  [ -L "$library" ] && continue
  base=$(basename "$library")
  install_name_tool -id "@loader_path/$base" "$library"
  while IFS= read -r dependency; do
    depbase=$(basename "$dependency")
    if [ -f "$FRAMEWORKS/$depbase" ]; then
        install_name_tool -change "$dependency" "@loader_path/$depbase" "$library"
    fi
  done <<EOF
$(otool -L "$library" | awk 'NR > 2 { sub(/^[[:space:]]+/, ""); sub(/ \(compatibility.*$/, ""); print }')
EOF
done

while IFS= read -r dependency; do
  depbase=$(basename "$dependency")
  if [ -f "$FRAMEWORKS/$depbase" ]; then
      install_name_tool -change "$dependency" "@rpath/$depbase" "$EXECUTABLE"
  fi
done <<EOF
$(otool -L "$EXECUTABLE" | awk 'NR > 1 { sub(/^[[:space:]]+/, ""); sub(/ \(compatibility.*$/, ""); print }')
EOF
install_name_tool -add_rpath @executable_path/../Frameworks "$EXECUTABLE" 2>/dev/null || true

find "$FRAMEWORKS" -type f -name '*.dylib' -exec codesign --force --sign - {} \;
cp "$PREFIX"/share/licenses/libmtp/* "$APP/Contents/Resources/licenses/"
cp "$PREFIX"/share/licenses/libusb/* "$APP/Contents/Resources/licenses/"
