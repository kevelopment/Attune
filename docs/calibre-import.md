# Importing a Calibre library

On first launch, choose **Import Calibre**. Attune checks common library locations; select a match, choose **Scan for libraries**, or browse with **Choose folder**. You can also import later through **Settings → Import Calibre library**. Select the folder containing `metadata.db`. Close Calibre while importing so its database and files remain consistent.

Attune reads the Calibre database read-only and copies its book files and covers into Attune’s library. It does not move or edit the source library, and Calibre does not need to be installed. This is a one-time import, not ongoing synchronization or export.

## What is imported

- One Attune book record for each Calibre record, including records without an available book file.
- Each available format file recorded by Calibre, kept byte-for-byte as a separate asset.
- Standard book metadata, covers, custom-column definitions and values, and reading positions, bookmarks, highlights, and notes. Resolvable annotations appear in the reader sidebar; unsupported or unplaced records remain available there with an unresolved status, and original Calibre payloads are retained.
- Stable Calibre library/book identity, so a repeat import can add missing files without replacing Attune’s edited metadata or preferred files.

Missing, corrupt, or unsafe files are reported per format; the remaining files for that book and other books can still import. Changed files are kept as additional variants. When no Calibre identity mapping exists, Attune reuses a book only if an exact content hash points to one unambiguous existing book. It never merges distinct books by title, author, or ISBN. Identical content already attached to another book is reported rather than moved.

Direct file import also accepts common ebook and document extensions. Built-in reading supports DRM-free EPUB, MOBI/AZW3, FB2, TXT, PDF, CBZ, and CBR. Other stored formats can be opened with the operating system’s associated app. Device profiles may support transferring formats Attune cannot read.

## Current limits

Attune imports Calibre reading state without replacing newer local progress. Only locators Attune can validate are navigable; unresolved records remain visible in the annotations sidebar. There is no Calibre export or live synchronization. EPUB files keep their original bytes; Attune’s separate cover is used in the library and does not rewrite the source file.

The optional library scan checks common home folders with limits on depth, time, and entry count. It skips hidden, system-cache, build, and symlink paths; use **Choose folder** for libraries elsewhere.

## Validation

The importer uses generated SQLite fixtures and isolated Calibre libraries. It opens the database read-only and checks database-relative book and format paths before copying. See the [capability matrix](capability-matrix.md) for the current tested scope.
