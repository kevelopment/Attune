# README screenshots

These screenshots show the actual Attune frontend in explicit browser preview mode (`?preview=1`). All book titles, author names, cover designs, and chapter text are fictional demonstration fixtures from `frontend/src/bridge.ts`. No imported books, personal metadata, or commercial cover artwork are included.

The original demonstration assets are part of Attune and covered by the repository's MIT license. To refresh them, run the frontend dev server, open preview mode, choose the library/details or chapter-preview view, and capture the app content without browser chrome. Keep screenshots representative of implemented features.

## Social preview

`social-preview.svg` is the editable 1280 × 640 source for the repository's sharing card. It embeds the Attune icon and fictional-library screenshot above. `social-preview.png` is the upload-ready image (under GitHub's 1 MB limit).

To render it on macOS:

```sh
sips -s format png docs/images/social-preview.svg --out docs/images/social-preview.png
```

GitHub uses a separately uploaded image: open the repository's **Settings → General → Social preview → Edit → Upload an image** and choose `social-preview.png`.
