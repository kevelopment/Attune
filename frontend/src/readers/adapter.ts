export type ReflowableFormat = 'epub' | 'mobi' | 'azw' | 'azw3' | 'fb2' | 'txt'
export type FixedFormat = 'pdf' | 'cbz' | 'cbr'
export type ReaderFormat = ReflowableFormat | FixedFormat

export type ReaderLayout = 'continuous' | 'single' | 'two-page'
export type ReaderTheme = 'light' | 'dark' | 'sepia' | 'gray' | 'charcoal'
export type ReaderFont = 'serif' | 'book' | 'sans' | 'mono'

export type ReaderSettings = {
  font: ReaderFont
  fontSize: number
  lineHeight: number
  padding: number
  layout: ReaderLayout
  theme: ReaderTheme
  fit: 'width' | 'page'
}

export type ReaderLocator =
  | { version: 1; kind: 'cfi'; value: string; progress: number }
  | { version: 1; kind: 'text-offset'; value: number; progress: number }
  | { version: 1; kind: 'page'; value: number; progress: number }

export type ReaderSection = { index: number; id: string; title: string }
export type ReaderSearchHit = { id: string; sectionIndex: number; excerpt: string; locator: ReaderLocator }
export type ReaderEvent =
  | { type: 'location'; locator: ReaderLocator; sectionIndex: number }
  | { type: 'sections'; sections: ReaderSection[] }
  | { type: 'error'; message: string }

// Format engines stay behind this small surface so the reader shell can keep
// the same controls, navigation and persistence behavior across engines.
export interface ReaderAdapter {
  readonly format: ReaderFormat
  readonly sections: ReaderSection[]
  open(container: HTMLElement, source: string, settings: ReaderSettings, signal?: AbortSignal): Promise<void>
  navigate(sectionIndex: number): Promise<void>
  next(): Promise<void>
  previous(): Promise<void>
  location(): ReaderLocator | null
  resolve(locator: ReaderLocator): Promise<boolean>
  restore(locator: ReaderLocator): Promise<boolean>
  search(query: string, signal?: AbortSignal): Promise<ReaderSearchHit[]>
  goToSearchHit(hit: ReaderSearchHit): Promise<void>
  applySettings(settings: ReaderSettings): void
  on(listener: (event: ReaderEvent) => void): () => void
  dispose(): void
}
