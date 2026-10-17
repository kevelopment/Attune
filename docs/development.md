# Developing Attune

## macOS setup

Use macOS 13+, Go 1.25+, Node.js 22 with npm, and the Xcode command-line tools. Install the native prerequisites described in the [Wails v2 installation guide](https://wails.io/docs/gettingstarted/installation).

```sh
./scripts/fetch-kindling.sh
./scripts/dev.sh
```

The helper download is pinned by version and SHA-256. Go/npm tooling and caches default to the checkout's ignored `.cache/` directory; the standard `GOPATH`, `GOMODCACHE`, `GOCACHE`, `GOBIN`, and `npm_config_cache` overrides are respected.

## Checks

```sh
./scripts/test.sh
```

This builds the frontend, runs Go tests with the race detector, and runs `go vet`. Tests use temporary data and simulated devices rather than connected hardware. Real-device compatibility still needs hardware testing.

## Desktop build

```sh
./scripts/build.sh
open build/bin/Attune.app
```

The local build includes Kindling and its license, then applies an ad-hoc signature on macOS. See [releasing](releasing.md) for the multi-platform build workflow.

## Browser preview

```sh
cd frontend
npm ci
npm run dev
```

Open the displayed local address with `?preview=1`. Preview mode uses fictional books and generated SVG covers. It cannot access the native library or write to an actual device. It is never enabled by default in the desktop app.

The README screenshots are captured from this preview, not from someone's personal library. See [screenshot notes](images/README.md).

## Project layout

| Path | Purpose |
| --- | --- |
| `app.go` | Wails bridge, import/edit/preview/transfer coordination |
| `internal/library` | SQLite metadata and managed originals |
| `internal/epub` | EPUB validation, metadata extraction, and sandboxed preview resources |
| `internal/device` | Mounted-volume validation and verified transfers |
| `internal/converter` | Kindling process and conversion cache |
| `frontend/src` | React interface, styles, and native/preview bridge |
| `frontend/public/attune.svg` | Editable app mark and browser favicon |
| `build/appicon.png` | Raster app icon used by Wails |
| `third_party/kindling` | Pinned helper metadata and upstream license |

## Local data

For compatibility with the first prototype, macOS data remains in `~/Library/Application Support/Ebook Library`. The product rename does not move or rewrite it. Use **Settings → Open library folder** to find the current path. Quit Attune before backing up this entire directory.

For an isolated desktop test, set `EBOOK_LIBRARY_HOME` to a temporary directory before launching the executable. `EBOOK_LIBRARY_CONVERTER` can override the helper path. These original override names remain supported.

Original EPUBs, databases, generated conversions, helper binaries, build output, and private test screenshots are ignored by Git. Keep public screenshot assets under `docs/images` limited to fictional or appropriately licensed material.
