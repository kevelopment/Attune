import type {
  ReaderAdapter,
  ReaderEvent,
  ReaderFont,
  ReaderFormat,
  ReaderLocator,
  ReaderSearchHit,
  ReaderSection,
  ReaderSettings,
} from '../adapter'

type FoliateLocation = { cfi?: string; fraction?: number; section?: { current?: number } }
type FoliateBook = {
  sections?: Array<{ id?: string; linear?: string }>
  toc?: Array<{ label?: string | Record<string, string>; href?: string; subitems?: FoliateBook['toc'] }>
  resolveHref?: (href: string) => { index?: number } | undefined
  transformTarget?: EventTarget
}
type FoliateView = HTMLElement & {
  book?: FoliateBook
  renderer?: HTMLElement & { setAttribute(name: string, value: string): void }
  lastLocation?: FoliateLocation
  resolveCFI?(cfi: string): { index?: number } | undefined
  open(source: string): Promise<void>
  init(options?: { lastLocation?: string; showTextStart?: boolean }): Promise<void>
  goTo(target: string | number | { fraction: number }): Promise<unknown>
  prev(): Promise<void>
  next(): Promise<void>
  search(options: { query: string }): AsyncIterable<unknown>
  clearSearch(): void
  close(): void
}

const themeColors: Record<ReaderSettings['theme'], [string, string, string]> = {
  light: ['#fff', '#2d2d31', '#4667bb'],
  dark: ['#1c1d20', '#e9e6df', '#a6baff'],
  sepia: ['#f2eadb', '#40372c', '#715e48'],
  gray: ['#bcbcbc', '#303030', '#4e4e4e'],
  charcoal: ['#303030', '#ededed', '#c2c2c2'],
}
const fontFamilies: Record<ReaderFont, string> = {
  serif: 'Georgia, "Times New Roman", serif',
  book: 'Palatino, "Book Antiqua", serif',
  sans: 'system-ui, -apple-system, sans-serif',
  mono: 'ui-monospace, SFMono-Regular, monospace',
}
const csp = `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'none'; style-src 'unsafe-inline' blob: data:; img-src blob: data:; font-src blob: data:; media-src blob: data:; connect-src 'none'; frame-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'">`

function sectionLabel(value: string | Record<string, string> | undefined, fallback: string) {
  if (typeof value === 'string') return value.trim() || fallback
  if (!value) return fallback
  return value[navigator.language] || value[navigator.language.split('-')[0]] || Object.values(value)[0] || fallback
}

function flattenTOC(items: FoliateBook['toc'], book: FoliateBook, names: Map<number, string>) {
  for (const item of items ?? []) {
    const target = item.href ? book.resolveHref?.(item.href) : undefined
    if (typeof target?.index === 'number' && !names.has(target.index)) names.set(target.index, sectionLabel(item.label, `Section ${target.index + 1}`))
    flattenTOC(item.subitems, book, names)
  }
}

function injectCSP(event: Event) {
  const detail = (event as CustomEvent<{ data: string | Blob | Promise<string | Blob>; type?: string }>).detail
  if (!detail || !/html|xhtml/i.test(detail.type ?? '')) return
  detail.data = Promise.resolve(detail.data).then(async value => {
    const html = value instanceof Blob ? await value.text() : String(value)
    if (/<meta\s+http-equiv=["']content-security-policy/i.test(html)) return html
    return /<head(?:\s[^>]*)?>/i.test(html)
      ? html.replace(/<head(?:\s[^>]*)?>/i, match => `${match}${csp}`)
      : `${csp}${html}`
  })
}

export class FoliateReflowableAdapter implements ReaderAdapter {
  readonly format: ReaderFormat
  readonly sections: ReaderSection[] = []
  private readonly listeners = new Set<(event: ReaderEvent) => void>()
  private host?: FoliateView
  private target?: EventTarget
  private container?: HTMLElement
  private activeLocator: ReaderLocator | null = null
  private onRelocate = (event: Event) => {
    const detail = (event as CustomEvent<FoliateLocation>).detail
    const cfi = detail?.cfi
    const sectionIndex = Math.max(0, detail?.section?.current ?? 0)
    if (!cfi) return
    this.activeLocator = { version: 1, kind: 'cfi', value: cfi, progress: clamp(detail.fraction ?? 0) }
    this.emit({ type: 'location', locator: this.activeLocator, sectionIndex })
  }

  constructor(format: Exclude<ReaderFormat, 'txt' | 'pdf' | 'cbz' | 'cbr'>) { this.format = format }

  async open(container: HTMLElement, source: string, settings: ReaderSettings, signal?: AbortSignal) {
    signal?.throwIfAborted()
    // @ts-expect-error Foliate.js intentionally ships native ESM without type declarations.
    await import('foliate-js/view.js')
    if (signal?.aborted) throw signal.reason
    this.container = container
    const host = document.createElement('foliate-view') as FoliateView
    host.style.cssText = 'display:block;width:100%;height:100%;min-height:0;overflow:hidden'
    this.host = host
    container.replaceChildren(host)
    host.addEventListener('relocate', this.onRelocate)
    await host.open(source)
    if (signal?.aborted) { this.dispose(); throw signal.reason }
    this.target = host.book?.transformTarget
    this.target?.addEventListener('data', injectCSP)
    this.updateSections()
    this.emit({ type: 'sections', sections: this.sections })
    host.addEventListener('load', this.onContentLoad as EventListener)
    this.applySettings(settings)
    await host.init({ showTextStart: true })
  }

  private onContentLoad = (event: Event) => {
    const detail = (event as CustomEvent<{ doc?: Document }>).detail
    if (detail?.doc) this.applyDocumentStyles(detail.doc)
  }

  private updateSections() {
    const book = this.host?.book
    const names = new Map<number, string>()
    if (book) flattenTOC(book.toc, book, names)
    this.sections.splice(0, this.sections.length, ...(book?.sections ?? []).map((section, index) => ({
      index,
      id: section.id || `section-${index}`,
      title: names.get(index) || `Section ${index + 1}`,
    })))
  }

  async navigate(sectionIndex: number) {
    if (!Number.isInteger(sectionIndex) || sectionIndex < 0 || sectionIndex >= this.sections.length) return
    await this.host?.goTo(sectionIndex)
  }
  async next() { await this.host?.next() }
  async previous() { await this.host?.prev() }
  location() { return this.activeLocator }
  async resolve(locator: ReaderLocator) {
    if (locator.kind !== 'cfi' || !this.host?.resolveCFI) return false
    try { return Boolean(this.host.resolveCFI(locator.value)) } catch { return false }
  }
  async restore(locator: ReaderLocator) {
    if (locator.kind === 'cfi') {
      try {
        if (!this.host) return false
        const target = this.host.resolveCFI?.(locator.value)
        if (this.host.resolveCFI && !target) return false
        await this.host.goTo(locator.value)
        return true
      } catch { return false }
    }
    if (locator.kind === 'text-offset' && this.host?.goTo) {
      try { return Boolean(await this.host.goTo({ fraction: clamp(locator.progress) })) } catch { return false }
    }
    return false
  }

  async search(query: string, signal?: AbortSignal) {
    const needle = query.trim()
    if (!needle || !this.host) return []
    const results: ReaderSearchHit[] = []
    const seen = new Set<string>()
    const iterable = this.host.search({ query: needle })
    try {
      for await (const raw of iterable) {
        signal?.throwIfAborted()
        if (raw === 'done' || !raw || typeof raw !== 'object') continue
        const result = raw as { cfi?: string; excerpt?: string; index?: number; subitems?: Array<{ cfi?: string; excerpt?: string }> }
        const items = result.subitems?.map(item => ({ ...item, index: result.index })) ?? (result.cfi ? [result] : [])
        for (const item of items) {
          if (!item.cfi || seen.has(item.cfi)) continue
          seen.add(item.cfi)
          let sectionIndex = item.index ?? 0
          try { sectionIndex = this.host.resolveCFI?.(item.cfi)?.index ?? sectionIndex } catch { /* Keep engine section fallback. */ }
          results.push({
            id: item.cfi,
            sectionIndex,
            excerpt: item.excerpt?.trim() || needle,
            locator: { version: 1, kind: 'cfi', value: item.cfi, progress: 0 },
          })
        }
      }
    } finally {
      this.host.clearSearch()
    }
    return results
  }
  async goToSearchHit(hit: ReaderSearchHit) { await this.restore(hit.locator) }

  applySettings(settings: ReaderSettings) {
    this.lastSettings = settings
    const renderer = this.host?.renderer
    if (!renderer) return
    renderer.setAttribute('flow', settings.layout === 'continuous' ? 'scrolled' : 'paginated')
    renderer.setAttribute('max-column-count', settings.layout === 'two-page' ? '2' : '1')
    renderer.setAttribute('max-inline-size', '760px')
    renderer.setAttribute('margin', `${Math.max(18, settings.padding / 2)}px`)
    for (const { doc } of (renderer as unknown as { getContents?: () => Array<{ doc: Document }> }).getContents?.() ?? []) this.applyDocumentStyles(doc, settings)
    this.host?.style.setProperty('--reader-bg', themeColors[settings.theme][0])
  }

  private applyDocumentStyles(doc: Document, settings?: ReaderSettings) {
    const current = settings ?? this.lastSettings
    if (!current || !doc.head || !doc.body) return
    let style = doc.getElementById('attune-reader-foliate-style') as HTMLStyleElement | null
    if (!style) { style = doc.createElement('style'); style.id = 'attune-reader-foliate-style'; doc.head.append(style) }
    const [background, foreground, link] = themeColors[current.theme]
    style.textContent = `html,body{background:${background}!important;color:${foreground}!important}body,body *{font-family:${fontFamilies[current.font]}!important;color:${foreground}!important}body{font-size:${current.fontSize}px!important;line-height:${current.lineHeight}!important;padding-inline:${current.padding}px!important;box-sizing:border-box!important}p,li,blockquote{line-height:${current.lineHeight}!important}a{color:${link}!important}img,svg,video{max-width:100%;height:auto}`
  }
  private lastSettings?: ReaderSettings

  on(listener: (event: ReaderEvent) => void) { this.listeners.add(listener); return () => this.listeners.delete(listener) }
  private emit(event: ReaderEvent) { for (const listener of this.listeners) listener(event) }
  dispose() {
    this.target?.removeEventListener('data', injectCSP)
    this.host?.removeEventListener('relocate', this.onRelocate)
    this.host?.removeEventListener('load', this.onContentLoad as EventListener)
    this.host?.close()
    this.host?.remove()
    this.host = undefined
    this.target = undefined
    this.container?.replaceChildren()
    this.container = undefined
    this.sections.splice(0)
    this.activeLocator = null
  }
}

function clamp(value: number) { return Math.min(1, Math.max(0, Number.isFinite(value) ? value : 0)) }
