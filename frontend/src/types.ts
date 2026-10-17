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
  revision: number
}
export type BookMetadataEdit = Pick<Book, 'title' | 'authors' | 'publisher' | 'language' | 'published' | 'subjects' | 'series' | 'seriesIndex' | 'description'> & { publisher: string; language: string; published: string; subjects: string[]; series: string; seriesIndex: string; description: string }
export type Device = { id: string; name: string; root: string; freeBytes: number; totalBytes: number }
export type DeviceBook = { path: string; name: string; size: number; bookID: string; title?: string; authors?: string[]; language?: string; subjects?: string[]; coverURL?: string; publisher?: string; published?: string; identifiers?: Record<string, string>; description?: string; series?: string; seriesIndex?: string; authorSort?: string[]; titleSort?: string; pageCount?: number; addedAt?: string; modifiedAt?: string }
export type PreviewChapter = { id: string; title: string; href: string }
export type BookPreview = { book: Book; chapters: PreviewChapter[] }
export type Progress = { id: string; bookID: string; title: string; stage: string; message: string; current: number; total: number }
export type ImportResult = { imported: number; duplicates: number; errors: string[] }
export type ImportProgress = { current: number; total: number; filename: string; imported: number; duplicates: number; failed: number; stage: 'importing' | 'done' | 'error'; message?: string }
export type AppState = { books: Book[]; devices: Device[]; converterReady: boolean; converterVersion: string; libraryPath: string; updateAvailable?: boolean }

declare global {
  interface Window {
    go?: { main?: { App?: Record<string, (...args: any[]) => Promise<any>> } }
    runtime?: {
      EventsOn?: (event: string, cb: (...args: any[]) => void) => (() => void) | void
      OnFileDrop?: (cb: (x: number, y: number, paths: string[]) => void, useDropTarget?: boolean) => void
      OnFileDropOff?: () => void
    }
  }
}
