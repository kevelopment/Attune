# Importing a Calibre library

On first launch, choose **Import Calibre** on the welcome screen. Attune checks common library locations; select a match, choose **Scan for libraries**, or browse with **Choose folder**. You can also import later through **Settings → Import Calibre library → Choose library…**. Select the library folder containing `metadata.db`. Close Calibre first so its metadata and files are consistent while they are copied.

Attune reads the Calibre database without migrating or editing it. EPUB files and covers are copied into the existing Attune library. Your Calibre library remains usable in Calibre, and Attune continues to use its own library layout. Calibre does not need to be installed.

The optional scan checks Documents, Desktop, Downloads, and other home folders. It is bounded by depth, time, and entry count, skips hidden/system-cache/build folders and symlinks, and does not search the entire computer. Use **Choose folder** for libraries elsewhere.

Choose **Start from scratch → Get started** to begin with an empty library. Setup is remembered, and users who already have books go straight to their library. Cancelling or failing an import keeps setup open; a partial import can be reviewed before continuing.

## What is imported

- EPUB files, without changing their bytes.
- Calibre titles and ordered authors, tags (shown as genres), publisher, publication date, language, description, identifiers, series and series index, author/title sort metadata, and Calibre’s native page count when available.
- The library’s cover, when available, instead of an older embedded EPUB cover.
- One import batch in Recently added, using the time the import occurred.

Books with no EPUB are counted as skipped. Invalid or missing files are reported per book; other books can continue importing. Byte-identical EPUB duplicates are counted separately, and existing Attune metadata and covers are kept unchanged.

## Current scope

This imports a snapshot rather than connecting or synchronizing the two libraries. Changes made later in either app do not propagate. Export to Calibre is deferred.

Calibre-specific features such as arbitrary custom columns, ratings, annotations, saved searches, virtual libraries, reading positions, and additional ebook formats are not imported. Matching titles, authors, and ISBNs do not merge distinct EPUB files. The source retains its unsupported metadata and formats. Attune currently represents one language per book.

## Validation

The importer is tested with generated database fixtures and an isolated library created by Calibre 9.16. Compatibility relies on the standard Calibre tables; it does not copy Calibre code or require its runtime. Database paths and format filenames are checked to reject files outside the selected library.
