# Third-party notices

Attune’s own source code is licensed under MIT. The project license is `LICENSE` in the source repository and `Attune-LICENSE.txt` in release packages. This applies to Attune-authored code and does not relicense bundled third-party components.

Release packages include full license texts and attributions for Attune’s compiled Go/runtime dependencies and bundled frontend runtime in `THIRD_PARTY_LICENSES.txt`. Versions come from `go.mod`/`go.sum` and `frontend/package-lock.json`; the file also includes the Go standard-library license.

Attune bundles the upstream Kindling v0.45.6 executable. Its MIT license is `third_party/kindling/LICENSE` in the source repository and `Kindling-LICENSE.txt` in release packages. Per-crate license expressions and notices from the upstream v0.45.6 `Cargo.lock` are in `third_party/kindling/THIRD_PARTY_LICENSES.txt` and `Kindling-THIRD_PARTY_LICENSES.txt`. The crate inventory retains OR license choices, includes full MPL-2.0 terms, and provides exact versioned crates.io source archive links to the source form for the relevant crates alongside the upstream Kindling source tag. Rust standard-library MIT/Apache-2.0 terms are included in `Kindling-THIRD_PARTY_LICENSES.txt`; the upstream release does not record its exact Rust compiler version. The four official Kindling release asset SHA-256 values are pinned in `scripts/kindling-pins.sh` and match the official release API digests.

Attune also bundles these reader and archive components in its frontend/runtime packages; their full license texts are included in `THIRD_PARTY_LICENSES.txt` and release packages:

- Foliate.js, pinned to commit `78914aef4466eb960965702401634c2cb348e9b1` (MIT).
- PDF.js (`pdfjs-dist` 6.4.299) (Apache-2.0).
- rardecode/v2 2.4.5 (BSD-2-Clause).

The macOS release bundles source-built libmtp 1.1.23 and libusb 1.0.30 under LGPL-2.1-or-later. Their exact source archive URLs and SHA-256 pins are in `scripts/macos-mtp-pins.sh`; full license texts ship in the app's `Contents/Resources/licenses` directory. Linux links to the distribution's libmtp runtime and does not bundle that library.
