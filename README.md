<img src="frontend/public/attune.svg" width="80" height="80" alt="Attune bonfire icon">

# Attune

Attune your library.

A local macOS EPUB library with automatic legacy MOBI conversion for original Kindle 1 and 2 devices. Built with Go, Wails v2, React, TypeScript, and SQLite. Kindling is bundled as a standalone conversion helper; Calibre is not required.

## Run the local app

Build on macOS 13 or newer with Go, Node.js/npm, and the Xcode command line tools installed:

```sh
./scripts/fetch-kindling.sh
./scripts/build.sh
open "build/bin/Attune.app"
```

The first helper download is pinned and checksum verified. The resulting app includes the helper and its license; end users do not install Kindling or Rust. The current helper download targets Apple Silicon. Intel packaging requires a separately pinned Intel helper and verification on Intel hardware.

For development, use `./scripts/dev.sh`. For a browser-only interface preview, run `npm ci && npm run dev` inside `frontend`, then open the displayed address with `?preview=1`. Preview data is simulated and never enabled by default in the desktop app. `frontend/design.html` is the initial layout sketch.

## Use

1. Import DRM-free EPUB books using **Import books** or drag them into the window. A status panel shows batch progress, the current filename, and imported, duplicate, and failed counts. Originals are copied into the managed library; source files stay untouched.
2. Browse, search, sort, or switch between grid and list. Author, genre, and language filters are shared between Library and Kindle views; genre uses embedded subject metadata, with Unknown for missing fields. Select a book to open a separate details panel; library books support **Read preview** and editing title, authors, publisher, language, publication date, genres, series, and description. Each book’s three-dot menu offers preview, editing, device transfer, and removal; removing a book still requires confirmation.
3. Connect the Kindle using a data-capable USB cable. Select it in the sidebar, or choose its mounted volume manually. Books in nested folders are included. Device gallery/list views use locally extracted metadata and cover thumbnails when available; filenames and neutral covers remain as fallbacks. Hidden metadata files and `.sdr` companion folders are excluded.
4. Select library books and choose **Send to Kindle**. The app converts, copies, verifies, and records each transfer. Repeated unchanged sends skip existing verified copies.
5. Eject the Kindle before unplugging it. Removing a device book and removing a local library book are separate actions with confirmations.

The four-icon rail and global search/import toolbar share the native macOS translucent backdrop and system appearance. Details occupy their own rounded panel beside the library, keeping book entries visible; drag its divider to resize it. The library-folder shortcut lives in Settings. Details show available embedded publisher, publication date, identifiers, series, genres, description, sort metadata, and explicit page counts, alongside file size and timestamps. Missing fields are omitted. Metadata edits are saved in the library without changing the original EPUB; generated Kindle files currently receive title and author overrides.

Attune keeps `~/Library/Application Support/Ebook Library` as its data folder for compatibility with existing installations. Existing books remain in place; Attune does not move or rewrite the library during the product rename. Quit the app before copying this entire directory for a backup. Converted files are disposable caches; they can be regenerated from the originals. Use **Settings → Open library folder** to locate the data.

## Verify

```sh
./scripts/test.sh
```

The test suite covers EPUB validation/import, SQLite persistence and duplicates, metadata revisioning, conversion/cache behavior, safe filesystem transfers, and cancellation. Tests use temporary directories rather than connected hardware. A real Kindle is still required to validate its renderer, navigation, and USB compatibility.

For isolated desktop testing, set `EBOOK_LIBRARY_HOME` to a temporary directory before launching the app executable. `EBOOK_LIBRARY_CONVERTER` can override the helper path for development. These existing override names remain supported; production defaults to the helper inside the app bundle. Build tools and Go/npm caches default to this checkout's ignored `.cache/` directory, and can be overridden with the standard `GOPATH`, `GOMODCACHE`, `GOCACHE`, `GOBIN`, and `npm_config_cache` variables.

## Release scope

The first version targets original Kindle 1 and 2 through mounted USB storage and legacy MOBI exports. EPUB remains the canonical library format. The app lists existing Kindle ebook files, including MOBI, PRC, and AZW; it does not convert them back to EPUB or synchronize annotations, clippings, reading progress, or collections. A sandboxed EPUB preview shows chapters, local styles/images, and declared language from managed originals. It blocks scripts and remote content; it does not simulate Kindle rendering or decrypt device books. A full reader with progress/annotations, cloud services, PDF import, and DRM removal are outside this release.

Transfers verify the selected volume's identity and copied bytes. They use an exclusive atomic rename when the filesystem supports it, or an exclusive verified copy on filesystems that do not. Both paths refuse to overwrite existing device files.

Local builds use an ad-hoc signature. Public distribution needs a Developer ID identity, signing of the app and helper, and Apple notarization. Credentials are not included in this project. The third-party helper version, checksum, source, and license are recorded in `third_party/kindling` and the fetch script. The helper executable is fetched locally and is ignored by Git; the pinned checksum and upstream MIT license are retained separately.

## License

Attune is available under the MIT License in `LICENSE`. The Kindling helper is a separate third-party work under its own MIT license in `third_party/kindling/LICENSE`.
