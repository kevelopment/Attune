# Developing Attune

## macOS setup

Use macOS 13+, mise, and the Xcode command-line tools. Install the native prerequisites from the [Wails v2 installation guide](https://wails.io/docs/gettingstarted/installation). Mise selects Go 1.26.9 and Node.js 22.22.1. Run `mise trust` once if mise asks to trust `mise.toml`.

```sh
mise run up
mise run down
```

`up` starts Vite and the native Wails app on the host. React and CSS changes hot-reload through Vite on port 5183; Go changes rebuild and restart the app. The Wails development server listens on port 34115. The app stores its persistent, isolated SQLite library in `.cache/dev-library`, separate from your normal Attune library. The pinned Kindling helper is fetched and checksum-verified on first start. Logs are in `.cache/dev/wails.log` (`tail -f .cache/dev/wails.log`). `down` preserves the development library and dependencies. Choose another frontend port with `ATTUNE_DEV_PORT=5184 mise run up`, or a library location with `ATTUNE_DEV_LIBRARY_HOME=/path/to/dev-library mise run up`. `./scripts/dev.sh` is an alias for `mise run up`.

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

This optional host-only browser preview uses fictional books and generated SVG covers. Open the displayed local address with `?preview=1`. It cannot access the native library or write to an actual device. It is never enabled by default in the desktop app.

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

The default macOS library is `~/Library/Application Support/Ebook Library`, preserving data from the first prototype. Use **Settings → Library location → Change…** to select a parent folder. Attune copies books and metadata into its `Attune Library` subfolder, switches to that copy, and remembers the location across restarts. The previous folder is retained for recovery; conversion caches are regenerated as needed. The destination must be new or empty, and transfers must finish before changing it. Use **Open folder** to find the active library. Quit Attune before backing up its entire directory.

The selected path is stored separately in the system configuration directory under `Attune/settings.json`. Development runs keep this preference in `<EBOOK_LIBRARY_HOME>.settings.json`, preserving isolation from the normal library. If a configured library is unavailable, startup reports an error rather than creating an empty replacement.

For an isolated desktop test, set `EBOOK_LIBRARY_HOME` to a temporary directory before launching the executable. `EBOOK_LIBRARY_CONVERTER` can override the helper path. These original override names remain supported.

Original EPUBs, databases, generated conversions, helper binaries, build output, and private test screenshots are ignored by Git. Keep public screenshot assets under `docs/images` limited to fictional or appropriately licensed material.
