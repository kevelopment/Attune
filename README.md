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
  <a href="#bring-your-calibre-library">Import from Calibre</a> ·
  <a href="docs/development.md">Build from source</a> ·
  <a href="docs/releasing.md">Release guide</a>
</p>

<p align="center">
  <a href="docs/images/showcase.png"><img src="docs/images/showcase.svg" width="100%" alt="Attune showcase: library, welcome, Recently added, and a simulated Kindle in the foreground"></a>
</p>
<p align="center"><sub>Four views of one local library, including a simulated Kindle.</sub></p>

<details>
<summary>View individual screenshots</summary>

<table>
  <tr>
    <td width="50%"><img src="docs/images/welcome-screen-rounded.svg" alt="Welcome screen with a detected Calibre library"><br><sub>Welcome</sub></td>
    <td width="50%"><img src="docs/images/library-calibre-import-rounded.svg" alt="Library after importing classic books from Calibre"><br><sub>Library</sub></td>
  </tr>
  <tr>
    <td width="50%"><img src="docs/images/recently-added-rounded.svg" alt="Recently added timeline for the imported books"><br><sub>Recently added</sub></td>
    <td width="50%"><img src="docs/images/connected-kindle-mock-rounded.svg" alt="Simulated Kindle reader with local demonstration books"><br><sub>Simulated Kindle</sub></td>
  </tr>
</table>
</details>

## A little less library admin

A local home for your EPUBs: organize your collection, edit metadata and covers, preview books, and send them to a reader over USB. Your original files stay intact. No account or Calibre installation required.

### Bring your Calibre library

Start fresh or import EPUBs, metadata, and covers from an existing Calibre library. Attune copies books into its own library and leaves Calibre’s library untouched. [Import details](docs/calibre-import.md).

### A companion for your reader

Browse and manage a mounted USB Kindle, with automatic legacy MOBI conversion for the original Kindle 1 and 2. USB support is experimental on macOS, Windows, and Linux; physical-device testing is still limited. More reader families, including Tolino and Kobo, are planned.

## Download

Download Attune for your operating system and CPU from the [Releases page](https://github.com/kevelopment/Attune/releases). Packages include the conversion helper; no developer tools or Calibre installation are required.

| Platform | Package |
| --- | --- |
| macOS 13+ | DMG installer and updater ZIP |
| Windows | ZIP archive |
| Linux | Compressed application archive |

**macOS installation:**

1. For a DMG, open it and drag Attune to Applications. For a ZIP, extract it and move `Attune.app` into Applications.
2. If Gatekeeper says “Apple could not verify Attune,” choose **Done**, then open **System Settings → Privacy & Security** and select **Open Anyway** for Attune.
3. Choose **Open** in the confirmation prompt. See [Apple’s instructions](https://support.apple.com/en-us/102445) for details. Current macOS downloads are ad-hoc signed and not Apple-notarized, so this may be needed on first launch.

Windows downloads are not Authenticode-signed. Linux requires GTK 3 and WebKit2GTK 4.1; Windows requires WebView2. See the [release guide](docs/releasing.md) for installation and USB requirements.

## Software updates

Attune checks for updates on startup and every 15 minutes. When a new release is available, use the sidebar download icon to install it and restart when ready. Your library stays in place.

## Build from source

Attune uses **Go · Wails v2 · React · TypeScript · SQLite**. It uses the operating system's webview rather than shipping a separate browser engine. [Kindling](https://github.com/ciscoriordan/kindling) provides standalone EPUB-to-MOBI conversion.

Use `mise run dev` for a browser preview, `mise run test` for Go tests, and `mise run lint` for formatting, vet, and TypeScript checks. On macOS, `mise run up` starts the native app with React/CSS hot reload and Go rebuilds; `mise run down` stops it. `mise run build` builds the desktop app for the current platform. See [development setup](docs/development.md) for prerequisites and platform details.

See [releasing](docs/releasing.md) for the tag-driven build and publication workflow.

## Where things stand

Attune is an early-stage project focused on EPUB libraries and USB readers. PDF import and support for additional reader families are future work.

Cloud sync is not planned. Attune is designed to work locally without requiring an account.

For current capabilities and limits, see the [development guide](docs/development.md), [Calibre import guide](docs/calibre-import.md), and [release notes](https://github.com/kevelopment/Attune/releases).

## Contributing

All contributions are welcome. Small, focused changes are easier to review. Pull requests automatically run the required test and lint checks.

## License & acknowledgements

Attune takes inspiration from [Calibre](https://github.com/kovidgoyal/calibre), created by Kovid Goyal. Thank you to Kovid and the Calibre community for building the foundation that so many ebook tools and readers rely on.

[MIT](LICENSE), with separate [third-party notices](THIRD_PARTY_NOTICES.md). Thanks to [Wails](https://wails.io), [Kindling](https://github.com/ciscoriordan/kindling), and the open-source ebook community. The name is a small nod to resting and preparing for the next journey at a bonfire.
