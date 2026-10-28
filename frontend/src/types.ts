export type Book = {
  id: string
  title: string
  authors: string[]
  language: string
  sha256: string
  coverURL: string
  subjects?: string[]
  publisher?: string
  published?: string
  identifiers?: Record<string, string>
  series?: string
  seriesIndex?: string
  description?: string
  authorSort?: string[]
  titleSort?: string
  pageCount?: number
  fileSize?: number
  modifiedAt?: string
  addedAt: string
  importBatchID?: string
  files?: BookFile[]
  preferredFileID?: string
  customMetadata?: unknown
  revision: number
}
export type BookFile = { id: string; bookID: string; format: string; filename: string; relativePath?: string; sha256: string; size: number; preferred: boolean; canRead: boolean; canTransfer: boolean; canConvert: boolean }
export type BookMetadataEdit = Pick<Book, 'title' | 'authors' | 'publisher' | 'language' | 'published' | 'subjects' | 'series' | 'seriesIndex' | 'description'> & { publisher: string; language: string; published: string; subjects: string[]; series: string; seriesIndex: string; description: string }
export type Device = { id: string; name: string; root: string; freeBytes: number; totalBytes: number; transport?: string; profileID?: string; bookFolders?: string[]; transferFormats?: string[]; allowEPUBToMOBI?: boolean; requiresExplicitSelection?: boolean }
export type DevicePreferences = { folders: string[]; formats: string[]; customized: boolean }
export type DeviceBook = { path: string; name: string; size: number; bookID: string; canRead?: boolean; title?: string; authors?: string[]; language?: string; subjects?: string[]; coverURL?: string; publisher?: string; published?: string; identifiers?: Record<string, string>; description?: string; series?: string; seriesIndex?: string; authorSort?: string[]; titleSort?: string; pageCount?: number; addedAt?: string; modifiedAt?: string }
export type PreviewChapter = { id: string; title: string; href: string }
export type BookPreview = { book: Book; chapters: PreviewChapter[]; fileID?: string }
export type ReadingProgress = { bookID: string; chapterIndex: number; position: number; updatedAt?: string }
export type ReadingBookmark = { id: number; bookID: string; chapterIndex: number; position: number; createdAt: string }
export type ReaderLocation = { bookId: string; assetId: string; found: boolean; schemaVersion: number; locator: unknown; updatedAt?: string }
export type Progress = { id: string; bookID: string; title: string; stage: string; message: string; current: number; total: number }
export type ImportResult = { imported: number; duplicates: number; skipped?: number; errors: string[]; bookIDs: string[] }
export type ImportProgress = { current: number; total: number; filename: string; imported: number; duplicates: number; skipped?: number; failed: number; stage: 'importing' | 'done' | 'error'; message?: string }
export type AppState = { books: Book[]; devices: Device[]; converterReady: boolean; converterVersion: string; libraryPath: string }

export type UpdateState = {
  currentVersion: string
  latestVersion: string
  available: boolean
  downloaded: boolean
  status: 'idle' | 'checking' | 'available' | 'downloading' | 'ready' | 'error' | 'restarting'
  releaseURL: string
  releaseNotes: string
  progress: number
  received: number
  total: number
  error: string
}
