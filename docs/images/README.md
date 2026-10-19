# README screenshots

The main README uses four 2480 × 1640 captures (1240 × 820 at 2× scale):

- `welcome-screen.png`: welcome screen with a detected Calibre library, captured against an empty development library.
- `library-calibre-import.png`: library after importing the classic books from the real Project Gutenberg EPUBs in Downloads through the Go backend.
- `recently-added.png`: timeline for that same imported collection.
- `connected-kindle-mock.png`: device page with locally converted MOBIs from those books. A disposable Go build overlay supplies the screenshot-only device identity and capacity; no physical USB reader was used or tested.

The screenshots come from the Go-backed browser renderer at 1240 × 820, using the app's dark material backdrop (`#45464a`) to match the desktop window. `scripts/render-readme-showcase.mjs` regenerates the `*-rounded.svg` copies and the layered `showcase.svg` hero from the four PNGs. It embeds the original raster data, preserving each capture without README CSS. Run it after replacing any capture. The README links to `showcase.png`, a transparent 4400 × 3200 export of that SVG, for zooming. Re-export the PNG at the same dimensions whenever the composition changes.

`library-details.png` and `book-preview.png` are earlier browser-preview captures using fictional demonstration fixtures from `frontend/src/bridge.ts`. Their original demonstration artwork is covered by the repository’s MIT license. These captures include third-party book cover artwork; that artwork is not licensed under Attune’s MIT license.

Keep screenshots representative of implemented features. Device screenshots must be labelled when simulated.

## Social preview

`social-preview.svg` is the editable 1280 × 640 source for the repository's sharing card. It embeds the Attune icon and fictional-library screenshot above. `social-preview.png` is the upload-ready image (under GitHub's 1 MB limit).

To render it on macOS:

```sh
sips -s format png docs/images/social-preview.svg --out docs/images/social-preview.png
```

GitHub uses a separately uploaded image: open the repository's **Settings → General → Social preview → Edit → Upload an image** and choose `social-preview.png`.
