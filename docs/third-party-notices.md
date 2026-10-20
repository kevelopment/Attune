# Third-party notices

Attune includes the following third-party software for ebook reading and archive handling:

| Component | Version | License | Source |
| --- | --- | --- | --- |
| [Foliate.js](https://github.com/johnfactotum/foliate-js) | Commit `78914aef4466eb960965702401634c2cb348e9b1` | MIT | The pinned Git revision in `frontend/package.json` and its `LICENSE` file (copyright John Factotum, 2022). |
| [PDF.js](https://github.com/mozilla/pdf.js) (`pdfjs-dist`) | `6.4.299` | Apache-2.0 | npm package metadata and `LICENSE` in the installed distribution. |
| [rardecode](https://github.com/nwaples/rardecode) | `v2.4.5` | BSD-2-Clause | Go module metadata and `LICENSE` in the pinned module. |

The full license texts are included in the repository’s [`THIRD_PARTY_LICENSES.txt`](../THIRD_PARTY_LICENSES.txt), which release scripts copy into each package. Foliate.js is pinned to a Git commit because its API is not versioned as a stable release. Its package metadata reports version `0.0.0`; the commit above is the reproducible version identifier used by Attune.

PDF.js and rardecode are pinned to exact package versions in the frontend lockfile and `go.mod`, respectively. Their licenses permit redistribution subject to the notices and conditions in the included license texts.
