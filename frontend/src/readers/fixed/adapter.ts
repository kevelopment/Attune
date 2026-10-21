import type {
  FixedFormat,
  ReaderAdapter,
  ReaderEvent,
  ReaderLocator,
  ReaderSearchHit,
  ReaderSection,
  ReaderSettings,
} from '../adapter'

type PdfDocument = {
  numPages: number
  getPage(page: number): Promise<PdfPage>
  getMetadata(): Promise<{ info?: { Title?: string } }>
  getPageIndex?(ref: unknown): Promise<number>
  destroy(): Promise<void>
}
type PdfLoadingTask = { promise: Promise<PdfDocument>; destroy(): Promise<void> }
type PdfPage = {
  getViewport(options: { scale: number }): { width: number; height: number }
  render(options: { canvas: HTMLCanvasElement; canvasContext: CanvasRenderingContext2D; viewport: { width: number; height: number } }): { promise: Promise<void>; cancel(): void }
  getTextContent(): Promise<{ items: Array<{ str?: string; transform?: number[] }> }>
  cleanup(): void
}
type ComicPage = { url: string; filename: string }
type PageSize = { width: number; height: number }

const PAGE_CACHE_LIMIT = 5
const themeBackground: Record<ReaderSettings['theme'], string> = {
  light: '#fff', dark: '#1c1d20', sepia: '#f2eadb', gray: '#bcbcbc', charcoal: '#303030',
}

/** Renders PDF and comic pages into an isolated, script-free canvas surface. */
export class FixedLayoutAdapter implements ReaderAdapter {
  readonly sections: ReaderSection[] = []
  private readonly listeners = new Set<(event: ReaderEvent) => void>()
  private container?: HTMLElement
  private surface?: HTMLElement
  private settings?: ReaderSettings
  private pdf?: PdfDocument
  private pdfLoadingTask?: PdfLoadingTask
  private pages: ComicPage[] = []
  private sizes: PageSize[] = []
  private observer?: IntersectionObserver
  private resizeObserver?: ResizeObserver
  private rendered = new Map<number, { canvas: HTMLCanvasElement; cancel?: () => void }>()
  private activePage = 0
  private activeLocator: ReaderLocator | null = null
  private renderGeneration = 0
  private disposed = false
  private abortSignal?: AbortSignal
  private abortHandler?: () => void

  readonly format: FixedFormat

  constructor(format: FixedFormat) { this.format = format }

  async open(container: HTMLElement, source: string, settings: ReaderSettings, signal?: AbortSignal) {
    this.dispose()
    this.disposed = false
    this.container = container
    this.settings = settings
    this.abortSignal = signal
    signal?.throwIfAborted()

    const surface = document.createElement('div')
    surface.className = 'attune-fixed-reader'
    Object.assign(surface.style, {
      width: '100%', height: '100%', minHeight: '0', overflow: 'auto',
      boxSizing: 'border-box', display: 'grid', alignContent: 'start', justifyItems: 'center',
      gap: '16px', padding: `${Math.max(8, settings.padding / 2)}px`,
      background: themeBackground[settings.theme],
    })
    container.replaceChildren(surface)
    this.surface = surface
    this.installEvents(surface)
    this.abortHandler = () => this.dispose()
    signal?.addEventListener('abort', this.abortHandler, { once: true })

    if (this.format === 'pdf') await this.openPDF(source, signal)
    else await this.openComicManifest(source, signal)
    signal?.throwIfAborted()
    if (this.disposed) return
    this.createPageSlots()
    this.updateLayout()
    this.resizeObserver = new ResizeObserver(() => {
      this.updateLayout()
      if (!this.rendered.has(this.activePage)) void this.renderPage(this.activePage)
    })
    this.resizeObserver.observe(surface)
    this.emit({ type: 'sections', sections: this.sections })
    this.setLocation(0)
  }

  private async openPDF(source: string, signal?: AbortSignal) {
    // Keep PDF.js and its worker out of the initial application bundle.
    const [{ getDocument, GlobalWorkerOptions }, worker] = await Promise.all([
      import('pdfjs-dist'),
      import('pdfjs-dist/build/pdf.worker.min.mjs?url'),
    ])
    signal?.throwIfAborted()
    GlobalWorkerOptions.workerSrc = worker.default
    const task = getDocument({ url: source, withCredentials: true }) as unknown as PdfLoadingTask
    this.pdfLoadingTask = task
    const cancel = () => { void task.destroy().catch(() => {}) }
    signal?.addEventListener('abort', cancel, { once: true })
    let pdf: PdfDocument
    try { pdf = await task.promise }
    finally {
      signal?.removeEventListener('abort', cancel)
      if (this.pdfLoadingTask === task) this.pdfLoadingTask = undefined
    }
    if (signal?.aborted || this.disposed) { await pdf.destroy(); return }
    this.pdf = pdf
    const metadata = await pdf.getMetadata().catch(() => undefined)
    // Avoid walking every page before showing the first one. Geometry is
    // filled as nearby pages render, with this portrait ratio as a placeholder.
    this.sizes = Array.from({ length: pdf.numPages }, () => ({ width: 2, height: 3 }))
    this.sections.splice(0, this.sections.length, ...Array.from({ length: pdf.numPages }, (_, index) => ({
      index, id: `page-${index + 1}`, title: `Page ${index + 1}`,
    })))
    if (metadata?.info?.Title && this.sections.length) this.sections[0] = { ...this.sections[0], title: metadata.info.Title }
  }

  private async openComicManifest(source: string, signal?: AbortSignal) {
    const response = await fetch(source, { credentials: 'same-origin', signal })
    if (!response.ok) throw new Error(`Could not open comic pages (${response.status}).`)
    const data = await response.json() as { pages?: ComicPage[] }
    const pages = data.pages
    if (!Array.isArray(pages) || pages.length === 0 || pages.some(page => typeof page?.url !== 'string' || typeof page?.filename !== 'string')) {
      throw new Error('The comic archive did not contain any readable pages.')
    }
    this.pages = pages
    this.sizes = pages.map(() => ({ width: 2, height: 3 }))
    this.sections.splice(0, this.sections.length, ...pages.map((page, index) => ({
      index, id: `page-${index + 1}`, title: page.filename || `Page ${index + 1}`,
    })))
  }

  private createPageSlots() {
    const surface = this.surface
    if (!surface) return
    const fragment = document.createDocumentFragment()
    for (let index = 0; index < this.sections.length; index++) {
      const slot = document.createElement('div')
      slot.dataset.page = String(index)
      slot.setAttribute('aria-label', this.sections[index]?.title ?? `Page ${index + 1}`)
      Object.assign(slot.style, {
        position: 'relative', display: 'grid', placeItems: 'center', overflow: 'hidden',
        background: themeBackground[this.settings?.theme ?? 'light'],
        boxShadow: '0 3px 18px rgba(0,0,0,.18)', flex: 'none', scrollSnapAlign: 'start',
      })
      fragment.append(slot)
    }
    surface.replaceChildren(fragment)
    this.observer = new IntersectionObserver(entries => {
      for (const entry of entries) {
        const page = Number((entry.target as HTMLElement).dataset.page)
        if (!Number.isInteger(page)) continue
        if (entry.isIntersecting) {
          this.setLocation(page)
          void this.renderPage(page)
          if (page > 0) void this.renderPage(page - 1)
          if (page + 1 < this.sections.length) void this.renderPage(page + 1)
          if (page + 2 < this.sections.length && this.settings?.layout === 'two-page') void this.renderPage(page + 2)
        }
      }
    }, { root: surface, rootMargin: '120% 0px', threshold: 0.01 })
    for (const slot of surface.children) this.observer.observe(slot)
  }

  private installEvents(surface: HTMLElement) {
    surface.addEventListener('scroll', this.onScroll, { passive: true })
  }
  private onScroll = () => {
    const surface = this.surface
    if (!surface) return
    const center = surface.scrollTop + surface.clientHeight / 2
    let closest = 0, distance = Number.POSITIVE_INFINITY
    for (const slot of Array.from(surface.children) as HTMLElement[]) {
      const nextDistance = Math.abs(slot.offsetTop + slot.offsetHeight / 2 - center)
      if (nextDistance < distance) { closest = Number(slot.dataset.page); distance = nextDistance }
    }
    const page = this.pageAnchor(closest)
    if (page !== this.activePage) this.setLocation(page)
    this.pruneCache(closest)
  }

  private updateLayout() {
    const surface = this.surface
    const settings = this.settings
    if (!surface || !settings) return
    const width = Math.max(1, surface.clientWidth - Math.max(16, settings.padding))
    const height = Math.max(1, surface.clientHeight - Math.max(16, settings.padding))
    const twoPage = settings.layout === 'two-page'
    const paginated = settings.layout !== 'continuous'
    surface.style.gridTemplateColumns = twoPage ? 'repeat(2, minmax(0, 1fr))' : 'minmax(0, 1fr)'
    surface.style.gridAutoFlow = twoPage ? 'row' : 'row'
    surface.style.scrollSnapType = settings.layout === 'continuous' ? 'none' : 'y mandatory'
    for (let index = 0; index < surface.children.length; index++) {
      const slot = surface.children[index] as HTMLElement
      const size = this.sizes[index] ?? { width: 2, height: 3 }
      const cellWidth = twoPage ? Math.floor((width - 16) / 2) : width
      const scale = settings.fit === 'width'
        ? cellWidth / size.width
        : Math.min(cellWidth / size.width, height / size.height)
      const pageWidth = Math.max(1, Math.floor(size.width * scale))
      const pageHeight = Math.max(1, Math.floor(size.height * scale))
      slot.style.width = `${pageWidth}px`
      // A paginated page or spread owns a full viewport-height snap area.
      // Short landscape pages otherwise expose later spreads in the same view.
      slot.style.height = `${paginated ? Math.max(height, pageHeight) : pageHeight}px`
      slot.style.gridColumn = twoPage ? String(index % 2 + 1) : '1'
      slot.style.marginInline = 'auto'
      const canvas = this.rendered.get(index)?.canvas
      if (canvas) { canvas.style.width = '100%'; canvas.style.height = '100%' }
    }
  }

  private async renderPage(index: number) {
    if (index < 0 || index >= this.sections.length || this.rendered.has(index) || this.disposed) return
    const slot = this.surface?.querySelector<HTMLElement>(`[data-page="${index}"]`)
    if (!slot) return
    const canvas = document.createElement('canvas')
    canvas.setAttribute('aria-hidden', 'true')
    Object.assign(canvas.style, { width: '100%', height: '100%', objectFit: 'contain', display: 'block' })
    slot.replaceChildren(canvas)
    const record = { canvas, cancel: undefined as (() => void) | undefined }
    this.rendered.set(index, record)
    const generation = this.renderGeneration
    try {
      if (this.pdf) await this.renderPDFPage(index, canvas, record)
      else await this.renderComicPage(index, canvas)
      if (this.disposed || generation !== this.renderGeneration) return
      this.updateLayout()
      this.pruneCache(this.activePage)
    } catch (error) {
      if (this.disposed || generation !== this.renderGeneration) return
      this.rendered.delete(index)
      slot.replaceChildren()
      if (index === this.activePage) this.emit({ type: 'error', message: error instanceof Error ? error.message : 'Could not render this page.' })
    }
  }

  private async renderPDFPage(index: number, canvas: HTMLCanvasElement, record: { cancel?: () => void }) {
    const page = await this.pdf!.getPage(index + 1)
    const slot = canvas.parentElement
    if (!slot) return
    const naturalSize = page.getViewport({ scale: 1 })
    this.sizes[index] = { width: naturalSize.width, height: naturalSize.height }
    this.updateLayout()
    const size = this.sizes[index]
    const scale = Math.min(slot.clientWidth / size.width, slot.clientHeight / size.height)
    const viewport = page.getViewport({ scale: Math.max(0.1, scale * Math.min(2, window.devicePixelRatio || 1)) })
    canvas.width = Math.ceil(viewport.width)
    canvas.height = Math.ceil(viewport.height)
    const context = canvas.getContext('2d', { alpha: false })
    if (!context) throw new Error('Canvas rendering is unavailable.')
    context.fillStyle = themeBackground[this.settings?.theme ?? 'light']
    context.fillRect(0, 0, canvas.width, canvas.height)
    const task = page.render({ canvas, canvasContext: context, viewport })
    record.cancel = () => task.cancel()
    await task.promise
    page.cleanup()
  }

  private async renderComicPage(index: number, canvas: HTMLCanvasElement) {
    const item = this.pages[index]
    const response = await fetch(item.url, { credentials: 'same-origin', signal: this.abortSignal })
    if (!response.ok) throw new Error(`Could not load ${item.filename}.`)
    const bitmap = await createImageBitmap(await response.blob())
    this.sizes[index] = { width: bitmap.width, height: bitmap.height }
    const slot = canvas.parentElement
    if (!slot) { bitmap.close(); return }
    const scale = Math.min(slot.clientWidth / bitmap.width, slot.clientHeight / bitmap.height, 2 * (window.devicePixelRatio || 1))
    canvas.width = Math.max(1, Math.floor(bitmap.width * scale))
    canvas.height = Math.max(1, Math.floor(bitmap.height * scale))
    const context = canvas.getContext('2d', { alpha: false })
    if (!context) { bitmap.close(); throw new Error('Canvas rendering is unavailable.') }
    context.fillStyle = themeBackground[this.settings?.theme ?? 'light']
    context.fillRect(0, 0, canvas.width, canvas.height)
    context.drawImage(bitmap, 0, 0, canvas.width, canvas.height)
    bitmap.close()
  }

  private pruneCache(center: number) {
    for (const [index, record] of this.rendered) {
      if (Math.abs(index - center) <= PAGE_CACHE_LIMIT / 2) continue
      record.cancel?.()
      record.canvas.remove()
      this.rendered.delete(index)
    }
  }

  private setLocation(index: number) {
    const page = this.pageAnchor(index)
    if (!this.sections.length) return
    this.activePage = page
    const progress = this.sections.length > 1 ? page / (this.sections.length - 1) : 0
    this.activeLocator = { version: 1, kind: 'page', value: page, progress }
    this.emit({ type: 'location', locator: this.activeLocator, sectionIndex: page })
  }

  private pageAnchor(index: number) {
    const page = Math.max(0, Math.min(this.sections.length - 1, index))
    return this.settings?.layout === 'two-page' ? Math.floor(page / 2) * 2 : page
  }

  async navigate(sectionIndex: number) {
    if (!Number.isInteger(sectionIndex) || sectionIndex < 0 || sectionIndex >= this.sections.length) return
    this.surface?.querySelector<HTMLElement>(`[data-page="${sectionIndex}"]`)?.scrollIntoView({ block: 'start', behavior: 'smooth' })
    this.setLocation(sectionIndex)
    await this.renderPage(sectionIndex)
  }
  async next() { await this.navigate(Math.min(this.sections.length - 1, this.activePage + (this.settings?.layout === 'two-page' ? 2 : 1))) }
  async previous() { await this.navigate(Math.max(0, this.activePage - (this.settings?.layout === 'two-page' ? 2 : 1))) }
  location() { return this.activeLocator }
  async resolve(locator: ReaderLocator) { return locator.kind === 'page' && Number.isInteger(locator.value) && locator.value >= 0 && locator.value < this.sections.length }
  async restore(locator: ReaderLocator) {
    if (locator.kind !== 'page') return false
    const index = Number.isInteger(locator.value) ? locator.value : Math.round(locator.progress * Math.max(0, this.sections.length - 1))
    if (index < 0 || index >= this.sections.length) return false
    await this.navigate(index)
    return true
  }

  async search(query: string, signal?: AbortSignal): Promise<ReaderSearchHit[]> {
    const needle = query.trim().toLocaleLowerCase()
    if (!needle || !this.pdf) return []
    const hits: ReaderSearchHit[] = []
    for (let index = 0; index < this.pdf.numPages; index++) {
      signal?.throwIfAborted()
      const page = await this.pdf.getPage(index + 1)
      const items = (await page.getTextContent()).items
      const text = items.map(item => item.str ?? '').join(' ')
      if (text.toLocaleLowerCase().includes(needle)) {
        const position = text.toLocaleLowerCase().indexOf(needle)
        hits.push({
          id: `page-${index + 1}-${position}`, sectionIndex: index,
          excerpt: text.slice(Math.max(0, position - 48), Math.min(text.length, position + needle.length + 72)).trim(),
          locator: { version: 1, kind: 'page', value: index, progress: this.sections.length > 1 ? index / (this.sections.length - 1) : 0 },
        })
      }
      page.cleanup()
    }
    return hits
  }
  async goToSearchHit(hit: ReaderSearchHit) { await this.restore(hit.locator) }

  applySettings(settings: ReaderSettings) {
    const themeChanged = settings.theme !== this.settings?.theme
    const layoutChanged = settings.layout !== this.settings?.layout
    const page = settings.layout === 'two-page' ? Math.floor(this.activePage / 2) * 2 : this.activePage
    this.settings = settings
    if (this.surface) {
      this.surface.style.background = themeBackground[settings.theme]
      this.surface.style.padding = `${Math.max(8, settings.padding / 2)}px`
      for (const slot of Array.from(this.surface.children) as HTMLElement[]) slot.style.background = themeBackground[settings.theme]
      this.updateLayout()
      if (layoutChanged) {
        this.surface.querySelector<HTMLElement>(`[data-page="${page}"]`)?.scrollIntoView({ block: 'start', behavior: 'instant' })
        this.setLocation(page)
      }
      if (themeChanged) {
        for (const [index, record] of this.rendered) {
          record.cancel?.()
          record.canvas.remove()
          const slot = this.surface.querySelector<HTMLElement>(`[data-page="${index}"]`)
          slot?.replaceChildren()
        }
        this.rendered.clear()
        void this.renderPage(this.activePage)
      }
    }
  }

  on(listener: (event: ReaderEvent) => void) { this.listeners.add(listener); return () => this.listeners.delete(listener) }
  private emit(event: ReaderEvent) { for (const listener of this.listeners) listener(event) }

  dispose() {
    if (this.disposed) return
    this.disposed = true
    this.renderGeneration++
    if (this.pdfLoadingTask) {
      void this.pdfLoadingTask.destroy().catch(() => {})
      this.pdfLoadingTask = undefined
    }
    this.abortSignal?.removeEventListener('abort', this.abortHandler!)
    this.abortSignal = undefined
    this.abortHandler = undefined
    this.observer?.disconnect()
    this.observer = undefined
    this.resizeObserver?.disconnect()
    this.resizeObserver = undefined
    this.surface?.removeEventListener('scroll', this.onScroll)
    for (const record of this.rendered.values()) record.cancel?.()
    this.rendered.clear()
    void this.pdf?.destroy()
    this.pdf = undefined
    this.surface?.remove()
    this.surface = undefined
    this.container?.replaceChildren()
    this.container = undefined
    this.sections.splice(0)
    this.pages = []
    this.sizes = []
    this.activeLocator = null
  }
}
