#!/bin/sh
set -eu

if [ "$(id -u)" -eq 0 ]; then
  echo "Run this installer as your regular user, without sudo." >&2
  exit 1
fi

home=${HOME:?HOME is not set}
source_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)
install_dir="$home/.local/opt/attune"
data_home=${XDG_DATA_HOME:-"$home/.local/share"}

case "$data_home" in
  /*) ;;
  *) echo "XDG_DATA_HOME must be an absolute path." >&2; exit 1 ;;
esac

for file in Attune kindling-cli INSTALL.txt attune.svg; do
  if [ ! -f "$source_dir/$file" ]; then
    echo "Missing $file beside this installer. Extract the complete Attune archive first." >&2
    exit 1
  fi
done
if [ ! -d "$source_dir/licenses" ]; then
  echo "Missing the licenses directory beside this installer." >&2
  exit 1
fi

mkdir -p "$install_dir" "$data_home/applications"
for file in Attune kindling-cli; do
  temp="$install_dir/.$file.tmp.$$"
  trap 'rm -f "$temp"' EXIT HUP INT TERM
  cp "$source_dir/$file" "$temp"
  chmod 755 "$temp"
  mv -f "$temp" "$install_dir/$file"
  trap - EXIT HUP INT TERM
done
cp "$source_dir/INSTALL.txt" "$source_dir/attune.svg" "$install_dir/"
rm -rf "$install_dir/licenses"
cp -R "$source_dir/licenses" "$install_dir/licenses"

desktop_quote() {
  escaped=$(printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g; s/%/%%/g')
  printf '"%s"' "$escaped"
}

desktop_file="$data_home/applications/attune.desktop"
{
  printf '%s\n' '[Desktop Entry]' 'Name=Attune' 'Comment=Manage and read your ebook library' 'Type=Application' 'Terminal=false' 'Categories=Office;Utility;'
  printf 'Exec=%s\n' "$(desktop_quote "$install_dir/Attune")"
  printf 'Icon=%s\n' "$(desktop_quote "$install_dir/attune.svg")"
  printf 'Path=%s\n' "$(desktop_quote "$install_dir")"
} > "$desktop_file"
chmod 644 "$desktop_file"

if command -v update-desktop-database >/dev/null 2>&1; then
  update-desktop-database "$data_home/applications" >/dev/null 2>&1 || true
fi

printf 'Attune is installed for your user at %s\n' "$install_dir"
printf 'Launch it from your application menu. In-app updates to this copy do not need administrator approval.\n'
printf 'After confirming it works, you may remove the system RPM/DEB if you no longer need that copy.\n'
