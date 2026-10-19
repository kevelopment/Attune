<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="frontend/public/attune-dark.svg">
    <img src="frontend/public/attune-light.svg" width="104" height="104" alt="Attune icon: a book page beside an e-reader">
  </picture>
</p>

<h1 align="center">Attune</h1>
<p align="center"><strong>Attune your library.</strong><br>A modern, open-source ebook manager and lightweight Calibre alternative.</p>

<p align="center">
  <a href="LICENSE"><img alt="MIT license" src="https://img.shields.io/badge/license-MIT-e8e4dc?style=flat-square&labelColor=292a2d"></a>
  <img alt="Local first" src="https://img.shields.io/badge/local-first-e8e4dc?style=flat-square&labelColor=292a2d">
  <a href="https://wails.io"><img alt="Built with Wails" src="https://img.shields.io/badge/built_with-Wails-e8e4dc?style=flat-square&labelColor=292a2d"></a>
</p>

<p align="center">
  <a href="https://github.com/kevelopment/Attune/releases/latest">Download latest release</a> ·
  <a href="#a-little-less-library-admin">Features</a> ·
  <a href="docs/development.md">Build from source</a> ·
  <a href="docs/releasing.md">Release guide</a>
</p>

<p align="center">
  <img src="docs/images/library-details.png" width="100%" alt="Attune library with illustrated fictional book covers and a separate book details panel">
  <br><sub>Staged preview with fictional books, authors, and original cover artwork. No personal library data.</sub>
</p>

## A little less library admin

Manage your EPUB library locally, edit metadata, preview chapters, and send books to Kindle over USB. No account or Calibre installation required. Kindle USB support is experimental and currently targets the original Kindle 1 and 2 using legacy MOBI conversion.

| Make room for your books | Take a closer look |
| --- | --- |
| Import DRM-free EPUBs by dropping files, using the picker, or pasting copied files (Linux needs `wl-paste` or `xclip`). Track progress and skip exact duplicates. | Browse a cover grid or compact list. Search, sort, and filter by author, genre, or language. |
| Correct titles, authors, publisher, dates, genres, series, and descriptions in a resizable details panel. | Preview chapters, local book styling, and images in a centered reading page. |
| Keep original EPUBs intact in an app-managed library, choose its location, and adjust text size. | Track imports, transfers, and ejections in session activity; choose book actions or select several books at once. |

### A small pause before the next chapter

<p align="center">
  <img src="docs/images/book-preview.png" width="100%" alt="Attune's chapter preview with a charcoal surround and a rounded white portrait page">
</p>

The preview is a quick way to check a book's contents and language before sending it. Scripts and remote content are blocked. It is not a simulation of a particular e-reader.

### A companion for your reader

Connect a mounted USB Kindle on macOS, Linux, or Windows to browse its books and available space, send books with automatic legacy MOBI conversion, remove device copies, or eject safely. Transfers verify copied bytes, refuse to overwrite existing files, and skip unchanged verified copies.

Book transfers currently use legacy MOBI, targeting the **original Kindle 1 and 2**. Support for more readers—including Tolino and Kobo—is planned. Experimental USB discovery, transfer, and eject are implemented for macOS, Linux, and Windows, but have not been validated with physical devices across those platforms. Linux requires `lsblk`, `udisksctl`, and permission to use UDisks; Windows support uses PowerShell storage cmdlets and volumes with drive letters.

## Download

Choose the latest release for your operating system and CPU from the [Releases page](https://github.com/kevelopment/Attune/releases). Release packages include the conversion helper and its license; you do not need Go, Node.js, Rust, or Calibre to run Attune. Older preview builds do not include the updater, so install a current release manually once.

| Platform | Features | Package |
| --- | --- | --- |
| macOS 13+ | Library and experimental USB Kindle management | DMG installer and updater ZIP |
| Windows | Library and experimental USB Kindle management | ZIP archive |
| Linux | Library and experimental USB Kindle management | Compressed application archive |

**macOS installation:**

1. For a DMG, open it and drag Attune to Applications. For a ZIP, extract it and move `Attune.app` into Applications.
2. If Gatekeeper says “Apple could not verify Attune,” choose **Done**, then open **System Settings → Privacy & Security** and select **Open Anyway** for Attune.
3. Choose **Open** in the confirmation prompt. See [Apple’s instructions](https://support.apple.com/en-us/102445) for details. Current macOS downloads are ad-hoc signed and not Apple-notarized, so this may be needed on first launch.

Windows downloads are not Authenticode-signed. Physical-device behavior has not been validated on every platform. Linux requires `lsblk`, `udisksctl`, and permission to use UDisks; Windows support uses PowerShell storage cmdlets and volumes with drive letters. Linux also requires GTK 3 and WebKit2GTK 4.1; Windows requires the WebView2 runtime. See the [release guide](docs/releasing.md) for details.

## Software updates

Attune checks public GitHub releases on startup and every 15 minutes while open; no account is required. When an update is available, click its download icon. Attune downloads and verifies the update before offering **Restart now** or **Later**. A staged update installs when you quit and is active the next time you open Attune; your library stays in place. The installed app folder must be writable. Older builds without the updater need a one-time manual download from the [Releases page](https://github.com/kevelopment/Attune/releases).

## Build from source

Attune uses **Go · Wails v2 · React · TypeScript · SQLite**. It uses the operating system's webview rather than shipping a separate browser engine. [Kindling](https://github.com/ciscoriordan/kindling) provides standalone EPUB-to-MOBI conversion.

Use `mise run dev` for a browser preview, `mise run test` for Go tests, and `mise run lint` for formatting, vet, and TypeScript checks. On macOS, `mise run up` starts the native app with React/CSS hot reload and Go rebuilds; `mise run down` stops it. `mise run build` builds the desktop app for the current platform. See [development setup](docs/development.md) for prerequisites and platform details.

See [development](docs/development.md) for prerequisites, tasks, browser preview, and the project layout. See [releasing](docs/releasing.md) for the tag-driven build and publication workflow.

## Where things stand

Attune is an early-stage project. Its library, metadata editor, book preview, updater, and experimental USB Kindle management for macOS, Linux, and Windows are implemented. PDF import and support for additional device families are future work.

Cloud sync is not planned. Attune is designed to work locally without requiring an account.

Metadata edits are stored in the library without changing the original EPUB. Generated Kindle files currently receive title and author overrides; other edited fields remain library metadata.

## Contributing

All contributions are welcome. Small, focused changes are easier to review. Pull requests automatically run the required test and lint checks.

## License & acknowledgements

[MIT](LICENSE), with separate [third-party notices](THIRD_PARTY_NOTICES.md). Thanks to [Wails](https://wails.io), [Kindling](https://github.com/ciscoriordan/kindling), and the open-source ebook community. The name is a small nod to resting and preparing for the next journey at a bonfire.
