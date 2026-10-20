# Unix MTP support

The Unix adapter uses transport ID `mtp-libmtp`. Release packages enable this adapter on macOS and Linux. Development builds enable it with the `libmtp` Go build tag.

To build the Unix adapter with cgo and the `libmtp` Go build tag:

- Debian/Ubuntu: `sudo apt install libmtp-dev pkg-config build-essential`
- Fedora: `sudo dnf install libmtp-devel pkgconf-pkg-config gcc`
- macOS development: install `pkgconf` and Xcode Command Line Tools. The build scripts download and checksum pinned libusb 1.0.30 and libmtp 1.1.23 source archives, compile them, and bundle the resulting libraries in the app.

On Linux, install `libmtp-dev`, `pkg-config`, and a C compiler; the app package requires the `libmtp` runtime library. Linux users may need a udev rule granting their account access to the device. Close other MTP clients when Attune reports that the interface is busy. Build with `CGO_ENABLED=1 go build -tags libmtp ./...`.

The binding uses libmtp's raw-device discovery and uncached device sessions, lists only configured folders, and stages individual file transfers through temporary files because libmtp's basic file API is path-based. C calls are synchronous, so context cancellation is checked before and after each native call; it cannot interrupt a transfer already inside libmtp. Uploads are checked against the declared byte count and the returned remote metadata. A failed native send is removed only when libmtp supplies the new object's ID. Exact source versions and SHA-256 pins are in `scripts/macos-mtp-pins.sh`; required LGPL license texts ship with the macOS app.
