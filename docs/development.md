# Developing Attune

## Platform prerequisites

Install mise and the native prerequisites from the [Wails v2 installation guide](https://wails.io/docs/gettingstarted/installation). Mise selects Go 1.26.9 and Node.js 22.22.1. Run `mise trust` once if mise asks to trust `mise.toml`.

On macOS, use macOS 13+, Xcode command-line tools, and `pkgconf` for the native MTP adapter. On Linux, install GTK 3, WebKit2GTK 4.1, libmtp, pkg-config, and a C compiler. Fetching the Linux ARM64 Kindling helper also requires Rust/Cargo 1.85 or newer because upstream does not publish an ARM64 binary. On Windows, install the MinGW-w64 UCRT64 GCC toolchain, PowerShell 7, and Visual Studio's Desktop development with C++ workload plus Windows SDK for the WPD helper. Linux USB storage also requires `lsblk`; eject needs `udisksctl` and working UDisks/polkit permissions. Windows USB storage uses PowerShell Storage CIM cmdlets and requires an external volume with a drive letter. Physical-device behavior still needs hardware testing; see the [capability matrix](capability-matrix.md).

## Common tasks

```sh
mise run test
mise run lint
mise run build
mise run dev
```

`test` runs Go tests with the race detector. `lint` checks Go formatting, runs `go vet`, and checks TypeScript without emitting files. `build` creates a desktop app for the current platform: a macOS app bundle, Linux archive, or Windows ZIP. Native builds need the platform prerequisites above.

`dev` runs the browser-only preview with fictional demo books and covers. Open the local address printed by Vite; it cannot access the native library or a physical device. Add `?preview=1&welcome=1` to preview first-run setup with fictional detected libraries and simulated scanning/importing. Without `welcome=1`, the preview opens the normal library.

The installed desktop app checks public GitHub releases and can download and stage an update. Browser preview does not have the native update action.

## macOS native development

```sh
mise run up
mise run down
```

`up` starts Vite and the native Wails app on the host. React and CSS changes hot-reload through Vite on port 5183; Go changes rebuild and restart the app. The Wails development server listens on port 34115. The app stores its persistent, isolated SQLite library in `.cache/dev-library`, separate from your normal Attune library. The pinned Kindling helper is fetched and checksum-verified on first start. Logs are in `.cache/dev/wails.log` (`tail -f .cache/dev/wails.log`). `down` preserves the development library and dependencies. Choose another frontend port with `ATTUNE_DEV_PORT=5184 mise run up`, or a library location with `ATTUNE_DEV_LIBRARY_HOME=/path/to/dev-library mise run up`. `./scripts/dev.sh` is an alias for `mise run up`.

Go tests use temporary data and simulated devices. Passing CI checks does not replace physical-device testing. The native `mise run up` and `mise run down` tasks use a separate development library and preserve it between runs.

Desktop builds include Kindling and its license. The macOS app receives an ad-hoc signature. See [releasing](releasing.md) for the multi-platform build workflow.

The README screenshots are captured from this preview, not from someone's personal library. See [screenshot notes](images/README.md).

## Project layout

| Path | Purpose |
| --- | --- |
| `app.go` | Wails bridge, import/edit/preview/transfer coordination |
| `internal/library` | SQLite metadata and managed originals |
| `internal/epub` | EPUB validation, metadata extraction, and sandboxed preview resources |
| `internal/device` | USB/MTP transports, device profiles, and verified transfers |
| `internal/formats` | Storage, reader, transfer, and conversion capability registry |
| `internal/converter` | Kindling process and conversion cache |
| `frontend/src` | React interface, styles, and native/preview bridge |
| `frontend/public/attune-dark.svg`, `attune-light.svg` | Editable app icon sources for dark and light appearance |
| `frontend/public/attune.svg` | Browser favicon that follows the system appearance |
| `build/appicon.png`, `appicon-light.png` | Raster icons rendered from the SVGs; Wails bundles the dark one, and macOS swaps the Dock icon to match the appearance while Attune runs |
| `third_party/kindling` | Pinned helper metadata and upstream license |

## Local data

The default macOS library is `~/Library/Application Support/Ebook Library`, preserving data from the first prototype. Use **Settings → Library location → Change…** to select a parent folder. Attune copies books and metadata into its `Attune Library` subfolder, switches to that copy, and remembers the location across restarts. The previous folder is retained for recovery; conversion caches are regenerated as needed. The destination must be new or empty, and transfers must finish before changing it. Use **Open folder** to find the active library. Quit Attune before backing up its entire directory.

The selected path is stored separately in the system configuration directory under `Attune/settings.json`. Development runs keep this preference in `<EBOOK_LIBRARY_HOME>.settings.json`, preserving isolation from the normal library. If a configured library is unavailable, startup reports an error rather than creating an empty replacement.

For an isolated desktop test, set `EBOOK_LIBRARY_HOME` to a temporary directory before launching the executable. `EBOOK_LIBRARY_CONVERTER` can override the helper path. These original override names remain supported.

Original EPUBs, databases, generated conversions, helper binaries, build output, and private test screenshots are ignored by Git. Keep public screenshot assets under `docs/images` limited to fictional or appropriately licensed material.
