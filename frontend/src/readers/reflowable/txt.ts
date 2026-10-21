import type { ReaderAdapter, ReaderEvent, ReaderLocator, ReaderSearchHit, ReaderSection, ReaderSettings } from '../adapter'

const section: ReaderSection = { index: 0, id: 'text', title: 'Text' }
const themes: Record<ReaderSettings['theme'], [string, string, string]> = {
  light: ['#fff', '#25262a', '#5278dc'],
  dark: ['#232428', '#e6e6e8', '#89a5ff'],
  sepia: ['#f2eadb', '#40372c', '#715e48'],
  gray: ['#bcbcbc', '#303030', '#4e4e4e'],
  charcoal: ['#303030', '#ededed', '#c2c2c2'],
}
const fonts: Record<ReaderSettings['font'], string> = {
  serif: 'Georgia, "Times New Roman", serif',
  book: 'Palatino, "Book Antiqua", serif',
  sans: 'system-ui, -apple-system, sans-serif',
  mono: 'ui-monospace, SFMono-Regular, monospace',
}

/** A small native-text adapter. TXT is paginated with CSS columns and keeps locators as character offsets. */
export class TextReaderAdapter implements ReaderAdapter {
  readonly format = 'txt' as const
  readonly sections = [section]
  private readonly listeners = new Set<(event: ReaderEvent) => void>()
  private root?: HTMLElement
  private viewport?: HTMLElement
  private content?: HTMLElement
  private text = ''
  private settings?: ReaderSettings
  private locator: ReaderLocator | null = null
  private resizeObserver?: ResizeObserver
  private suppressScroll = false

  async open(container: HTMLElement, source: string, settings: ReaderSettings, signal?: AbortSignal) {
    const response = await fetch(source, { signal })
    if (!response.ok) throw new Error(`Could not open text file (${response.status})`)
    const text = await response.text()
    signal?.throwIfAborted()
    this.text = text.replace(/^\uFEFF/, '').replace(/\r\n?/g, '\n')

    const root = document.createElement('div')
    root.className = 'attune-txt-reader'
    Object.assign(root.style, { display: 'flex', width: '100%', height: '100%', minWidth: '0', minHeight: '0', overflow: 'hidden' })
    const viewport = document.createElement('div')
    viewport.className = 'attune-txt-viewport'
    Object.assign(viewport.style, { width: '100%', height: '100%', minWidth: '0', minHeight: '0', overflow: 'auto', overscrollBehavior: 'contain' })
    const content = document.createElement('article')
    content.className = 'attune-txt-content'
    content.textContent = this.text
    viewport.append(content)
    root.append(viewport)
    container.replaceChildren(root)
    this.root = root
    this.viewport = viewport
    this.content = content
    this.settings = settings
    this.resizeObserver = new ResizeObserver(() => this.applySettings(this.settings!))
    this.resizeObserver.observe(viewport)
    viewport.addEventListener('scroll', this.onScroll, { passive: true })
    this.applySettings(settings)
    this.emit({ type: 'sections', sections: this.sections })
    this.updateLocation()
  }

  async navigate(sectionIndex: number) { if (sectionIndex === 0) this.scrollToFraction(0) }
  async next() { this.turn(1) }
  async previous() { this.turn(-1) }
  location() { return this.locator }
  async resolve(locator: ReaderLocator) {
    return locator.kind === 'text-offset' && locator.value >= 0 && locator.value <= this.text.length
  }

  async restore(locator: ReaderLocator) {
    if (!this.viewport || locator.kind !== 'text-offset') return false
    const fraction = this.text.length && locator.value > 0 ? clamp(locator.value / this.text.length) : clamp(locator.progress)
    this.scrollToFraction(fraction)
    return true
  }

  async search(query: string, signal?: AbortSignal) {
    const needle = query.trim().toLocaleLowerCase()
    if (!needle) return []
    const lower = this.text.toLocaleLowerCase()
    const hits: ReaderSearchHit[] = []
    let offset = 0
    while (hits.length < 500) {
      signal?.throwIfAborted()
      const found = lower.indexOf(needle, offset)
      if (found < 0) break
      const excerptStart = Math.max(0, found - 52)
      const excerptEnd = Math.min(this.text.length, found + needle.length + 76)
      const excerpt = this.text.slice(excerptStart, excerptEnd).replace(/\s+/g, ' ').trim()
      hits.push({
        id: `txt:${found}`,
        sectionIndex: 0,
        excerpt: `${excerptStart ? '…' : ''}${excerpt}${excerptEnd < this.text.length ? '…' : ''}`,
        locator: { version: 1, kind: 'text-offset', value: found, progress: this.text.length ? found / this.text.length : 0 },
      })
      offset = found + Math.max(needle.length, 1)
    }
    return hits
  }
  async goToSearchHit(hit: ReaderSearchHit) { await this.restore(hit.locator) }

  applySettings(settings: ReaderSettings) {
    this.settings = settings
    if (!this.root || !this.viewport || !this.content) return
    const [background, ink, link] = themes[settings.theme]
    const columns = settings.layout === 'continuous' ? 1 : settings.layout === 'two-page' ? 2 : 1
    const viewportWidth = Math.max(1, this.viewport.clientWidth)
    const pageWidth = columns === 2 ? Math.max(1, (viewportWidth - settings.padding * 2) / 2) : Math.max(1, viewportWidth - settings.padding * 2)
    const currentFraction = this.currentFraction()
    Object.assign(this.root.style, { background, color: ink })
    Object.assign(this.viewport.style, {
      overflowX: settings.layout === 'continuous' ? 'hidden' : 'auto',
      overflowY: settings.layout === 'continuous' ? 'auto' : 'hidden',
      scrollSnapType: settings.layout === 'continuous' ? 'y proximity' : 'x mandatory',
    })
    Object.assign(this.content.style, {
      boxSizing: 'border-box',
      width: settings.layout === 'continuous' ? 'min(100%, 760px)' : 'max-content',
      minWidth: settings.layout === 'continuous' ? '0' : '100%',
      maxWidth: settings.layout === 'continuous' ? '760px' : 'none',
      minHeight: settings.layout === 'continuous' ? '100%' : `${this.viewport.clientHeight}px`,
      height: settings.layout === 'continuous' ? 'auto' : `${this.viewport.clientHeight}px`,
      margin: settings.layout === 'continuous' ? '0 auto' : '0',
      padding: `${Math.max(24, settings.padding)}px ${Math.max(24, settings.padding)}px`,
      columnWidth: settings.layout === 'continuous' ? 'auto' : `${pageWidth}px`,
      columnCount: settings.layout === 'continuous' ? 'auto' : 'auto',
      columnGap: `${Math.max(36, settings.padding)}px`,
      columnFill: settings.layout === 'continuous' ? 'auto' : 'auto',
      fontFamily: fonts[settings.font],
      fontSize: `${settings.fontSize}px`,
      lineHeight: String(settings.lineHeight),
      whiteSpace: 'pre-wrap',
      overflowWrap: 'anywhere',
      color: ink,
      background: 'transparent',
      scrollSnapAlign: 'start',
      orphans: '2',
      widows: '2',
    })
    if (settings.layout !== 'continuous') {
      this.content.style.setProperty('column-fill', 'auto')
      this.content.style.setProperty('column-count', 'auto')
    }
    requestAnimationFrame(() => this.scrollToFraction(currentFraction, false))
    this.updateLocation()
    void link
  }

  on(listener: (event: ReaderEvent) => void) { this.listeners.add(listener); return () => this.listeners.delete(listener) }
  dispose() {
    this.resizeObserver?.disconnect()
    this.viewport?.removeEventListener('scroll', this.onScroll)
    this.root?.remove()
    this.root = undefined
    this.viewport = undefined
    this.content = undefined
    this.text = ''
    this.locator = null
  }

  private onScroll = () => { if (!this.suppressScroll) this.updateLocation() }
  private turn(direction: -1 | 1) {
    if (!this.viewport || !this.settings) return
    if (this.settings.layout === 'continuous') this.viewport.scrollBy({ top: direction * Math.max(1, this.viewport.clientHeight * 0.9), behavior: 'smooth' })
    else this.viewport.scrollBy({ left: direction * Math.max(1, this.viewport.clientWidth), behavior: 'smooth' })
  }
  private currentFraction() {
    if (!this.viewport) return this.locator?.progress ?? 0
    const max = this.settings?.layout === 'continuous'
      ? this.viewport.scrollHeight - this.viewport.clientHeight
      : this.viewport.scrollWidth - this.viewport.clientWidth
    const current = this.settings?.layout === 'continuous' ? this.viewport.scrollTop : this.viewport.scrollLeft
    return max > 0 ? clamp(current / max) : 0
  }
  private scrollToFraction(fraction: number, smooth = false) {
    if (!this.viewport) return
    const value = clamp(fraction)
    const vertical = this.settings?.layout === 'continuous'
    const max = vertical ? this.viewport.scrollHeight - this.viewport.clientHeight : this.viewport.scrollWidth - this.viewport.clientWidth
    this.suppressScroll = true
    this.viewport.scrollTo({ left: vertical ? 0 : max * value, top: vertical ? max * value : 0, behavior: smooth ? 'smooth' : 'instant' })
    requestAnimationFrame(() => { this.suppressScroll = false; this.updateLocation() })
  }
  private updateLocation() {
    const progress = this.currentFraction()
    const value = Math.round(progress * Math.max(0, this.text.length - 1))
    this.locator = { version: 1, kind: 'text-offset', value, progress }
    this.emit({ type: 'location', locator: this.locator, sectionIndex: 0 })
  }
  private emit(event: ReaderEvent) { for (const listener of this.listeners) listener(event) }
}

function clamp(value: number) { return Math.min(1, Math.max(0, Number.isFinite(value) ? value : 0)) }
