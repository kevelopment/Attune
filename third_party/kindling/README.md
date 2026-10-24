# Bundled Kindling converter

- Upstream: https://github.com/ciscoriordan/kindling
- Release: v0.45.6 (2026-09-21)
- Official release assets and verified SHA-256 digests:

  | Platform | Upstream asset | Source | SHA-256 |
  | --- | --- | --- | --- |
  | macOS arm64 | `kindling-cli-mac-apple-silicon` | [release asset](https://github.com/ciscoriordan/kindling/releases/download/v0.45.6/kindling-cli-mac-apple-silicon) | `3de02c4ed420e3ebb42f2a88a8c911dbcf179e41fbedaa77e901dddef8d362a8` |
  | macOS amd64 | `kindling-cli-mac-intel` | [release asset](https://github.com/ciscoriordan/kindling/releases/download/v0.45.6/kindling-cli-mac-intel) | `e4ebea49f37907c35ae3d7678a61840333bb01779705424219c731c230a844dc` |
  | Linux amd64 | `kindling-cli-linux` | [release asset](https://github.com/ciscoriordan/kindling/releases/download/v0.45.6/kindling-cli-linux) | `417ad6dad4b1072fabb20610f320ba44ca8f4afcbe2f47948dcefc3955bd6439` |
  | Linux arm64 | Built from upstream tag `v0.45.6` with `cargo install --locked` | [source tag](https://github.com/ciscoriordan/kindling/tree/v0.45.6) | Source build; no upstream ARM64 release asset |
  | Windows amd64 | `kindling-cli-windows.exe` | [release asset](https://github.com/ciscoriordan/kindling/releases/download/v0.45.6/kindling-cli-windows.exe) | `1988a319d0d9af2e75cf776eb9e63a59412fcd8ebfc5d0cebb000519ccd99f54` |

  Fetch scripts verify the official release digest before placing a prebuilt helper under its platform directory. Linux ARM64 builds the same pinned upstream tag from source because that release does not publish an ARM64 binary.
- License: MIT; see `LICENSE` (copied from the same upstream tag).

The release binary reports `kindling 0.45.6`. The app runs `build INPUT -o OUTPUT --legacy-mobi --no-validate` for dual MOBI7+KF8 output, then `rewrite-metadata` with `--title` and repeatable `--author` flags for library edits. Publishing-only checks are skipped to allow personal books without covers; EPUB input and legacy MOBI output are validated by the app.
