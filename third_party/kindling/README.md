# Bundled Kindling converter

- Upstream: https://github.com/ciscoriordan/kindling
- Release: v0.45.6 (2026-09-21)
- Asset: `kindling-cli-mac-apple-silicon`
- Source: https://github.com/ciscoriordan/kindling/releases/download/v0.45.6/kindling-cli-mac-apple-silicon
- SHA-256: `3de02c4ed420e3ebb42f2a88a8c911dbcf179e41fbedaa77e901dddef8d362a8`
- License: MIT; see `LICENSE` (copied from the same upstream tag).

The release binary reports `kindling 0.45.6`. The app runs `build INPUT -o OUTPUT --legacy-mobi --no-validate` for dual MOBI7+KF8 output, then `rewrite-metadata` with `--title` and repeatable `--author` flags for library edits. Publishing-only checks are skipped to allow personal books without covers; EPUB input and legacy MOBI output are validated by the app.
