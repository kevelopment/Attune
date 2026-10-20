#!/bin/sh
set -eu
APP_ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
. "$APP_ROOT/scripts/macos-mtp-pins.sh"
ARCH=${1:-$(uname -m)}
case "$ARCH" in
  amd64) ARCH=x86_64 ;;
  arm64|x86_64) ;;
  *) echo "unsupported macOS MTP architecture: $ARCH" >&2; exit 2 ;;
esac
PREFIX=${ATTUNE_MTP_PREFIX:-$APP_ROOT/.cache/macos-mtp/$ARCH}
CACHE="$APP_ROOT/.cache/macos-mtp/sources"
mkdir -p "$CACHE" "$PREFIX"

download() {
  url=$1 file=$2 checksum=$3
  if [ ! -f "$file" ] || [ "$(shasum -a 256 "$file" | awk '{print $1}')" != "$checksum" ]; then
    rm -f "$file"
    curl --fail --location --retry 3 "$url" -o "$file"
  fi
  actual=$(shasum -a 256 "$file" | awk '{print $1}')
  [ "$actual" = "$checksum" ] || { echo "SHA-256 mismatch for $file" >&2; exit 1; }
}

download "$LIBUSB_URL" "$CACHE/libusb-$LIBUSB_VERSION.tar.bz2" "$LIBUSB_SHA256"
download "$LIBMTP_URL" "$CACHE/libmtp-$LIBMTP_VERSION.tar.gz" "$LIBMTP_SHA256"
if [ ! -d "$CACHE/libusb-$LIBUSB_VERSION" ]; then
  tar -xjf "$CACHE/libusb-$LIBUSB_VERSION.tar.bz2" -C "$CACHE"
fi
if [ ! -d "$CACHE/libmtp-$LIBMTP_VERSION" ]; then
  tar -xzf "$CACHE/libmtp-$LIBMTP_VERSION.tar.gz" -C "$CACHE"
fi

if [ ! -f "$PREFIX/lib/pkgconfig/libmtp.pc" ] || [ ! -f "$PREFIX/lib/libmtp.dylib" ]; then
  rm -rf "$CACHE/libusb-$LIBUSB_VERSION" "$CACHE/libmtp-$LIBMTP_VERSION"
  tar -xjf "$CACHE/libusb-$LIBUSB_VERSION.tar.bz2" -C "$CACHE"
  tar -xzf "$CACHE/libmtp-$LIBMTP_VERSION.tar.gz" -C "$CACHE"

  export MACOSX_DEPLOYMENT_TARGET=13.0
  export CFLAGS="${CFLAGS:-} -O2 -arch $ARCH -mmacosx-version-min=13.0"
  export LDFLAGS="${LDFLAGS:-} -arch $ARCH -mmacosx-version-min=13.0 -Wl,-headerpad_max_install_names"
  # libtool probes ARG_MAX with sysctl on macOS. Sandboxed builds can deny
  # that probe, leaving max_cmd_len empty and causing broken partial links.
  export lt_cv_sys_max_cmd_len=65536
  export PKG_CONFIG_PATH="$PREFIX/lib/pkgconfig${PKG_CONFIG_PATH:+:$PKG_CONFIG_PATH}"

  (
    cd "$CACHE/libusb-$LIBUSB_VERSION"
    # The current host SDK may expose APIs newer than the deployment target.
    # Force libusb's portable fallback for pipe2, which appeared after macOS 13.
    ac_cv_func_pipe2=no ./configure --prefix="$PREFIX" --disable-static --enable-shared
    make -j"${ATTUNE_BUILD_JOBS:-2}"
    make install
  )
  (
    cd "$CACHE/libmtp-$LIBMTP_VERSION"
    ./configure --prefix="$PREFIX" --disable-static --enable-shared --disable-mtpz --disable-silent-rules
    make -j"${ATTUNE_BUILD_JOBS:-2}"
    make install
  )
fi

mkdir -p "$PREFIX/share/licenses/libmtp" "$PREFIX/share/licenses/libusb"
for file in COPYING COPYING.LIB; do
  [ ! -f "$CACHE/libmtp-$LIBMTP_VERSION/$file" ] || cp "$CACHE/libmtp-$LIBMTP_VERSION/$file" "$PREFIX/share/licenses/libmtp/libmtp-$file"
  [ ! -f "$CACHE/libusb-$LIBUSB_VERSION/$file" ] || cp "$CACHE/libusb-$LIBUSB_VERSION/$file" "$PREFIX/share/licenses/libusb/libusb-$file"
done
for license in "$PREFIX"/share/licenses/libmtp/* "$PREFIX"/share/licenses/libusb/*; do
  [ -f "$license" ] || { echo "missing MTP dependency license text" >&2; exit 1; }
done

PKG_CONFIG_PATH="$PREFIX/lib/pkgconfig${PKG_CONFIG_PATH:+:$PKG_CONFIG_PATH}" pkg-config --exists libmtp || {
  echo "Pinned libmtp build did not produce libmtp.pc" >&2
  exit 1
}
printf '%s\n' "$PREFIX"
