import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import {
  BookOpen, Clock3, HardDrive, Settings2, Search, Plus, Grid2X2, List, ArrowDownUp, ArrowLeft, ArrowRight,
  MoreHorizontal, X, Pencil, Trash2, Send, RefreshCw, Power, Check, AlertCircle,
  CircleHelp, FolderOpen, Upload, CheckCircle2, LoaderCircle, PanelRightClose,
} from 'lucide-react'
import { bridge, progressFromEvent } from './bridge'
import type { AppState, Book, BookMetadataEdit, BookPreview, DeviceBook, Progress, ImportProgress, ImportResult } from './types'

type Page = 'library' | 'recent' | 'device' | 'settings'
type Sort = 'recent' | 'title' | 'author'
type Confirmation = { title: string; description: string; confirmLabel: string; action: () => void }
type EditDraft = { title: string; authors: string; publisher: string; language: string; published: string; subjects: string; series: string; seriesIndex: string; description: string }
type MenuAnchor = { key: string; left: number; top: number }
const fmtBytes = (n: number) => n < 1_000 ? `${n} B` : n < 1_000_000 ? `${(n / 1_000).toFixed(0)} KB` : n < 1_000_000_000 ? `${(n / 1_000_000).toFixed(1)} MB` : `${(n / 1_000_000_000).toFixed(1)} GB`

export default function App() {
  const [state, setState] = useState<AppState | null>(null)
  const [navigation, setNavigation] = useState<{ pages: Page[]; index: number }>({ pages: ['library'], index: 0 })
  const page = navigation.pages[navigation.index]
  const [query, setQuery] = useState('')
  const [sort, setSort] = useState<Sort>('recent')
  const [layout, setLayout] = useState<'grid' | 'list'>('grid')
  const [authorFilter, setAuthorFilter] = useState('')
  const [genreFilter, setGenreFilter] = useState('')
  const [languageFilter, setLanguageFilter] = useState('')
  const [selected, setSelected] = useState<string[]>([])
  const [focusedBook, setFocusedBook] = useState<Book | null>(null)
  const [focusedDeviceBook, setFocusedDeviceBook] = useState<DeviceBook | null>(null)
  const [detailWidth, setDetailWidth] = useState(260)
  const [deviceBooks, setDeviceBooks] = useState<DeviceBook[]>([])
  const [deviceBooksLoading, setDeviceBooksLoading] = useState(false)
  const [deviceBooksError, setDeviceBooksError] = useState('')
  const [deviceSelection, setDeviceSelection] = useState<string[]>([])
  const [confirmation, setConfirmation] = useState<Confirmation | null>(null)
  const [previewTarget, setPreviewTarget] = useState<Book | null>(null)
  const [bookPreview, setBookPreview] = useState<BookPreview | null>(null)
  const [previewLoading, setPreviewLoading] = useState(false)
  const [previewError, setPreviewError] = useState('')
  const [previewChapter, setPreviewChapter] = useState(0)
  const [progress, setProgress] = useState<Progress | null>(null)
  const [importProgress, setImportProgress] = useState<ImportProgress | null>(null)
  const [toast, setToast] = useState<{ text: string; kind?: 'error' } | null>(null)
  const [busy, setBusy] = useState(false)
  const [dragging, setDragging] = useState(false)
  const [editing, setEditing] = useState(false)
  const [editDraft, setEditDraft] = useState<EditDraft>({ title: '', authors: '', publisher: '', language: '', published: '', subjects: '', series: '', seriesIndex: '', description: '' })
  const [activeMenu, setActiveMenu] = useState<MenuAnchor | null>(null)
  const searchRef = useRef<HTMLInputElement>(null)
  const importGuard = useRef(false)
  const transferGuard = useRef(false)
  const refreshGuard = useRef(false)
  const busyRef = useRef(false)
  const importPathsRef = useRef<(paths: string[]) => void>(() => {})
  const deviceRefreshRef = useRef<() => void>(() => {})
  const deviceBooksOwner = useRef<string | null>(null)
  busyRef.current = busy

  function navigateTo(nextPage: Page) {
    setNavigation(current => {
      if (current.pages[current.index] === nextPage) return current
      const pages = [...current.pages.slice(0, current.index + 1), nextPage]
      return { pages, index: pages.length - 1 }
    })
  }

  const refresh = useCallback(async (force = false) => {
    if (refreshGuard.current || (importGuard.current && !force)) return
    refreshGuard.current = true
    try { setState(await bridge.getState()) }
    catch (e) { setToast({ text: message(e), kind: 'error' }) }
    finally { refreshGuard.current = false }
  }, [])
  useEffect(() => { void refresh() }, [refresh])
  useEffect(() => {
    const timer = window.setInterval(() => { if (!busyRef.current && !importGuard.current) void refresh() }, 6000)
    return () => window.clearInterval(timer)
  }, [refresh])
  useEffect(() => {
    const unsub = [bridge.on('transfer:progress', (p: Progress) => {
      const next = progressFromEvent(p)
      setProgress(next)
      if (next.stage === 'error' || next.stage === 'cancelled' || (next.stage === 'done' && (!next.total || next.current >= next.total))) transferGuard.current = false
    }), bridge.on('import:progress', (p: ImportProgress) => { if (importGuard.current) setImportProgress(p) }), bridge.on('library:changed', () => void refresh()), bridge.on('devices:changed', () => void refresh())]
    const nativeDrop = window.runtime?.OnFileDrop
    const drop = (e: DragEvent) => { e.preventDefault(); setDragging(false); const paths = Array.from(e.dataTransfer?.files ?? []).map((f: any) => f.path).filter(Boolean); if (paths.length) importPathsRef.current(paths) }
    const over = (e: DragEvent) => { e.preventDefault(); setDragging(true) }
    const leave = (e: DragEvent) => { if (!e.relatedTarget) setDragging(false) }
    if (nativeDrop) nativeDrop((_x, _y, paths) => { setDragging(false); importPathsRef.current(paths) }, true)
    else { window.addEventListener('drop', drop); window.addEventListener('dragover', over); window.addEventListener('dragleave', leave) }
    return () => { unsub.forEach(fn => typeof fn === 'function' && fn()); if (nativeDrop) window.runtime?.OnFileDropOff?.(); else { window.removeEventListener('drop', drop); window.removeEventListener('dragover', over); window.removeEventListener('dragleave', leave) } }
  }, [refresh])
  useEffect(() => { if (toast) { const timer = window.setTimeout(() => setToast(null), 5200); return () => clearTimeout(timer) } }, [toast])
  useEffect(() => {
    if (!activeMenu) return
    const outside = (event: PointerEvent) => { if (!(event.target as Element | null)?.closest('[data-book-menu]')) setActiveMenu(null) }
    document.addEventListener('pointerdown', outside)
    const dismiss = () => setActiveMenu(null)
    window.addEventListener('resize', dismiss)
    window.addEventListener('scroll', dismiss, true)
    return () => { document.removeEventListener('pointerdown', outside); window.removeEventListener('resize', dismiss); window.removeEventListener('scroll', dismiss, true) }
  }, [activeMenu])

  const metadata = useMemo(() => {
    const items = [...(state?.books ?? []), ...deviceBooks]
    const values = (key: 'authors' | 'subjects' | 'language') => {
      const found = items.flatMap(item => {
        const raw = item[key]
        return (Array.isArray(raw) ? raw : typeof raw === 'string' && raw ? [raw] : []).map(x => x.trim()).filter(Boolean)
      })
      const missing = items.some(item => {
        const raw = item[key]
        return Array.isArray(raw) ? raw.length === 0 : typeof raw !== 'string' || raw.trim() === ''
      })
      return { values: [...new Set(found)].sort((a, b) => a.localeCompare(b)), missing }
    }
    return { authors: values('authors'), genres: values('subjects'), languages: values('language') }
  }, [state?.books, deviceBooks])
  const books = useMemo(() => {
    const all = state?.books ?? []
    const needle = query.trim().toLowerCase()
    return all.filter(b => (!needle || `${b.title} ${b.authors.join(' ')}`.toLowerCase().includes(needle)) && matchesFilter(b.authors, authorFilter) && matchesFilter(b.subjects, genreFilter) && matchesFilter(b.language ? [b.language] : [], languageFilter) && (page !== 'recent' || Date.now() - new Date(b.addedAt).getTime() < 30 * 86400000))
      .sort((a, b) => sort === 'title' ? a.title.localeCompare(b.title) : sort === 'author' ? (a.authors[0] ?? '').localeCompare(b.authors[0] ?? '') : b.addedAt.localeCompare(a.addedAt))
  }, [state?.books, query, page, sort, authorFilter, genreFilter, languageFilter])
  const device = state?.devices?.[0]
  const isDevice = page === 'device'
  const visibleDeviceBooks = useMemo(() => deviceBooks.filter(book => {
    const needle = query.trim().toLowerCase()
    const title = book.title || fileTitle(book.name)
    return (!needle || `${title} ${book.name} ${(book.authors ?? []).join(' ')}`.toLowerCase().includes(needle)) && matchesFilter(book.authors, authorFilter) && matchesFilter(book.subjects, genreFilter) && matchesFilter(book.language ? [book.language] : [], languageFilter)
  }).sort((a, b) => sort === 'title' ? (a.title || fileTitle(a.name)).localeCompare(b.title || fileTitle(b.name)) : sort === 'author' ? (a.authors?.[0] ?? '').localeCompare(b.authors?.[0] ?? '') : 0), [deviceBooks, query, authorFilter, genreFilter, languageFilter, sort])
  const transferActive = transferGuard.current || (!!progress && !['error', 'cancelled'].includes(progress.stage) && !(progress.stage === 'done' && (!progress.total || progress.current >= progress.total)))
  useEffect(() => {
    const keys = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement | null
      const typing = !!target && (target.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName))
      if (confirmation && e.key !== 'Escape') return
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'f') { e.preventDefault(); searchRef.current?.focus() }
      if (e.key === 'Escape') {
        if (activeMenu) { setActiveMenu(null); e.preventDefault(); return }
        if (confirmation) setConfirmation(null)
        else if (previewTarget) { setPreviewTarget(null); setBookPreview(null) }
        else { setFocusedBook(null); setFocusedDeviceBook(null); setEditing(false); setSelected([]) }
      }
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'a' && !typing) {
        e.preventDefault()
        if (page === 'device') setDeviceSelection(visibleDeviceBooks.map(b => b.path))
        else if (page === 'library' || page === 'recent') setSelected(books.map(b => b.id))
      }
    }
    window.addEventListener('keydown', keys)
    return () => window.removeEventListener('keydown', keys)
  }, [page, visibleDeviceBooks, books, confirmation, previewTarget, activeMenu])

  useEffect(() => {
    setDeviceSelection([])
    if (!device) { setDeviceBooks([]); setDeviceBooksLoading(false); setDeviceBooksError(''); deviceBooksOwner.current = null; deviceRefreshRef.current = () => {}; return }
    if (!isDevice) {
      if (deviceBooksOwner.current && deviceBooksOwner.current !== device.id) { setDeviceBooks([]); deviceBooksOwner.current = null }
      setDeviceBooksLoading(false)
      deviceRefreshRef.current = () => {}
      return
    }
    let disposed = false
    let inFlight = false
    const hasCachedBooks = deviceBooksOwner.current === device.id
    if (!hasCachedBooks) { setDeviceBooks([]); setDeviceBooksLoading(true); setDeviceBooksError('') }
    const load = async () => {
      if (disposed || inFlight) return
      inFlight = true
      try {
        const result = await bridge.getDeviceBooks(device.id)
        if (!disposed) { setDeviceBooks(result); setDeviceBooksError(''); deviceBooksOwner.current = device.id }
      } catch (e) {
        if (!disposed) setDeviceBooksError(message(e))
      } finally {
        inFlight = false
        if (!disposed) setDeviceBooksLoading(false)
      }
    }
    deviceRefreshRef.current = () => { void load() }
    void load()
    const timer = window.setInterval(() => { void load() }, 6000)
    return () => { disposed = true; window.clearInterval(timer); deviceRefreshRef.current = () => {} }
  }, [device?.id, isDevice])

  async function run(action: () => Promise<any>, success?: (result: any) => void | Promise<void>) {
    setBusy(true)
    try { const result = await action(); await success?.(result); return result }
    catch (e) { setToast({ text: message(e), kind: 'error' }); return null }
    finally { setBusy(false) }
  }
  async function performImport(action: () => Promise<ImportResult>, total = 0, filename = '') {
    if (importGuard.current) return
    importGuard.current = true
    setBusy(true)
    setImportProgress({ current: 0, total, filename: filename || 'Choose EPUB files…', imported: 0, duplicates: 0, failed: 0, stage: 'importing' })
    try {
      const result = await action()
      const errors = result?.errors ?? []
      const imported = result?.imported ?? 0
      const duplicates = result?.duplicates ?? 0
      if (!imported && !duplicates && !errors.length) setImportProgress(null)
      else setImportProgress(current => ({ current: current?.total ? current.total : current?.current ?? 0, total: current?.total ?? total, filename: current?.filename ?? filename, imported, duplicates, failed: errors.length, stage: 'done', message: errors[0] }))
      await refresh(true)
    } catch (e) {
      setImportProgress(current => ({ current: current?.current ?? 0, total: current?.total ?? total, filename: current?.filename ?? filename, imported: current?.imported ?? 0, duplicates: current?.duplicates ?? 0, failed: current?.failed ?? 0, stage: 'error', message: message(e) }))
    } finally {
      importGuard.current = false
      setBusy(false)
    }
  }
  async function importPaths(paths: string[]) {
    if (!paths.length) return
    await performImport(() => bridge.importPaths(paths), paths.length, paths[0].split(/[\\/]/).pop() ?? '')
  }
  importPathsRef.current = paths => { void importPaths(paths) }
  async function beginImport() {
    if (importGuard.current) return
    if (bridge.preview) await performImport(() => bridge.previewImport(), 18, 'Preparing sample EPUBs…')
    else await performImport(() => bridge.importBooks())
  }
  async function addDevice() { await run(() => bridge.selectDevice(), async () => { await refresh(); navigateTo('device') }) }
  async function startEdit(book: Book) {
    setFocusedDeviceBook(null)
    setFocusedBook(book)
    setEditDraft({ title: book.title, authors: book.authors.join('; '), publisher: book.publisher ?? '', language: book.language ?? '', published: book.published ?? '', subjects: (book.subjects ?? []).join('; '), series: book.series ?? '', seriesIndex: book.seriesIndex ?? '', description: book.description ?? '' })
    setEditing(true)
  }
  async function openPreview(book: Book) {
    setPreviewTarget(book)
    setBookPreview(null)
    setPreviewChapter(0)
    setPreviewError('')
    setPreviewLoading(true)
    try { setBookPreview(await bridge.getBookPreview(book.id)) }
    catch (e) { setPreviewError(message(e)) }
    finally { setPreviewLoading(false) }
  }
  function previewDeviceBook(bookID: string) {
    const local = (state?.books ?? []).find(book => book.id === bookID)
    if (local) { setActiveMenu(null); void openPreview(local) }
  }
  async function saveEdit() {
    if (!focusedBook || !editDraft.title.trim()) return
    const edit: BookMetadataEdit = { title: editDraft.title.trim(), authors: editDraft.authors.split(/[;\n]/).map(x => x.trim()).filter(Boolean), publisher: editDraft.publisher.trim(), language: editDraft.language.trim(), published: editDraft.published.trim(), subjects: editDraft.subjects.split(/[;\n]/).map(x => x.trim()).filter(Boolean), series: editDraft.series.trim(), seriesIndex: editDraft.seriesIndex.trim(), description: editDraft.description.trim() }
    await run(() => bridge.updateBookMetadata(focusedBook.id, edit), async (book: Book) => { setEditing(false); setFocusedBook(book); await refresh() })
  }
  async function deleteLocal(book: Book) {
    setConfirmation({ title: 'Remove from library?', description: `“${book.title}” will be removed from your library. Any copy already on a Kindle will stay there.`, confirmLabel: 'Remove from library', action: () => { void run(() => bridge.deleteBook(book.id), async () => { setSelected(s => s.filter(id => id !== book.id)); setFocusedBook(null); await refresh(); setToast({ text: 'Book removed from your library' }) }) } })
  }
  async function deleteSelected() {
    const targets = (state?.books ?? []).filter(b => selected.includes(b.id))
    if (!targets.length) return
    setConfirmation({ title: `Remove ${targets.length} ${targets.length === 1 ? 'book' : 'books'} from library?`, description: 'Any copies already on a Kindle will stay there.', confirmLabel: 'Remove from library', action: () => { void run(async () => { for (const book of targets) await bridge.deleteBook(book.id) }, async () => { setSelected([]); await refresh(); setToast({ text: `${targets.length} ${targets.length === 1 ? 'book' : 'books'} removed from your library` }) }) } })
  }
  async function send(ids: string[]) {
    if (transferActive) { setToast({ text: 'Wait for the current Kindle transfer to finish before starting another.' }); return }
    if (!device) { setToast({ text: 'Connect a Kindle first, then choose it in Devices.', kind: 'error' }); return }
    transferGuard.current = true
    setProgress({ id: 'starting', bookID: '', title: ids.length === 1 ? books.find(b => b.id === ids[0])?.title ?? 'Books' : `${ids.length} books`, stage: 'queued', message: 'Preparing transfer', current: 0, total: ids.length })
    const ok = await run(() => bridge.sendBooks(device.id, ids), () => setSelected([]))
    if (ok === null) { transferGuard.current = false; setProgress(p => p ? { ...p, stage: 'error', message: 'Could not start transfer. Check the Kindle connection and try again.' } : null) }
  }
  function eject() {
    if (!device) return
    setConfirmation({ title: 'Eject Kindle?', description: 'Finish writing to the device and disconnect it safely.', confirmLabel: 'Eject Kindle', action: () => { void run(() => bridge.ejectDevice(device.id), async () => { setToast({ text: 'Kindle ejected safely' }); await refresh() }) } })
  }
  function confirmRemoveFromDevice(paths: string[]) {
    if (!device || !paths.length) return
    const single = paths.length === 1
    setConfirmation({ title: `Remove ${paths.length} ${single ? 'book' : 'books'} from Kindle?`, description: 'Your library copies will stay on this Mac.', confirmLabel: 'Remove from Kindle', action: () => { void run(() => bridge.removeDeviceBooks(device.id, paths), async () => { setDeviceSelection(s => s.filter(path => !paths.includes(path))); setDeviceBooks(await bridge.getDeviceBooks(device.id)); setToast({ text: `${single ? 'Book' : 'Books'} removed from Kindle` }) }) } })
  }
  function removeFromDevice() { confirmRemoveFromDevice([...deviceSelection]) }
  function toggle(id: string, e?: React.MouseEvent) { setSelected(s => s.includes(id) ? s.filter(x => x !== id) : e?.shiftKey ? [...s, id] : [...s, id]) }

  const hasFilters = !!(authorFilter || genreFilter || languageFilter)
  const openDeviceDetails = (book: DeviceBook) => { setFocusedBook(null); setFocusedDeviceBook(book) }
  function toggleMenu(key: string, button: HTMLButtonElement) {
    if (activeMenu?.key === key) { setActiveMenu(null); return }
    const rect = button.getBoundingClientRect()
    const width = 220
    setActiveMenu({ key, left: Math.max(8, Math.min(window.innerWidth - width - 8, rect.right - width)), top: Math.max(8, Math.min(window.innerHeight - 190, rect.bottom + 6)) })
  }
  const detailsOpen = !!(focusedBook || focusedDeviceBook)
  const resizeHandle = <ResizeHandle width={detailWidth} onWidthChange={setDetailWidth}/>
  return <div className={`app-shell ${bridge.preview ? 'preview-mode' : ''} ${detailsOpen ? 'details-open' : ''}`} style={detailsOpen ? { gridTemplateColumns: `64px minmax(320px, 1fr) ${detailWidth}px` } : undefined}>
    <aside className="sidebar" aria-label="Main navigation">
      <nav className="rail-nav" aria-label="Library sections">
        <button className={`rail-item ${page === 'library' ? 'active' : ''}`} title="All books" aria-label="All books" aria-current={page === 'library' ? 'page' : undefined} onClick={() => navigateTo('library')}><BookOpen/></button>
        <button className={`rail-item ${page === 'recent' ? 'active' : ''}`} title="Recently added" aria-label="Recently added" aria-current={page === 'recent' ? 'page' : undefined} onClick={() => navigateTo('recent')}><Clock3/></button>
        <button className={`rail-item ${page === 'device' ? 'active' : ''}`} title={device?.name ?? 'Add a Kindle'} aria-label={device?.name ?? 'Add a Kindle'} aria-current={page === 'device' ? 'page' : undefined} onClick={() => device ? navigateTo('device') : void addDevice()}>{device ? <span className="device-glyph"><KindleIcon/><i/></span> : <HardDrive/>}</button>
      </nav>
      <div className="rail-spacer"/>
      <button className={`rail-item rail-settings ${page === 'settings' ? 'active' : ''}`} title="Settings" aria-label="Settings" aria-current={page === 'settings' ? 'page' : undefined} onClick={() => navigateTo('settings')}><Settings2/></button>
    </aside>

    <header className="topbar">
      <div className="topbar-navigation" aria-label="Navigation history">
        <button className="history-button" aria-label="Back" title="Back" disabled={navigation.index === 0} onClick={() => setNavigation(current => ({ ...current, index: Math.max(0, current.index - 1) }))}><ArrowLeft size={17}/></button>
        <button className="history-button" aria-label="Forward" title="Forward" disabled={navigation.index >= navigation.pages.length - 1} onClick={() => setNavigation(current => ({ ...current, index: Math.min(current.pages.length - 1, current.index + 1) }))}><ArrowRight size={17}/></button>
      </div>
      <div className="top-actions">
        {page !== 'settings' && <label className="search-box"><Search size={17}/><input ref={searchRef} placeholder="Search books" aria-label="Search books" value={query} onChange={e => setQuery(e.target.value)}/><kbd>⌘ F</kbd>{query && <button className="clear-search" onClick={() => setQuery('')} aria-label="Clear search"><X size={14}/></button>}</label>}
        {page === 'device' && device && <button className="toolbar-button" title="Refresh Kindle contents" onClick={() => deviceRefreshRef.current()}><RefreshCw size={16}/><span>Refresh</span></button>}
        {page !== 'settings' && <button className="toolbar-button primary-button" onClick={() => void beginImport()} disabled={busy}><Plus size={17}/><span>Import books</span></button>}
      </div>
    </header>

    <main className="main-panel">
      {page === 'settings' ? <Settings state={state} onOpenFolder={() => void run(() => bridge.openLibraryFolder())}/> : <>
        {isDevice && device ? <section className="device-overview">
          <div className="device-hero-icon"><HardDrive size={25}/></div><div className="device-copy"><div className="eyebrow">CONNECTED DEVICE</div><h1>{device.name}</h1><div className="device-path">{device.root}</div></div>
          <div className="storage"><div className="storage-line"><span>{fmtBytes(device.freeBytes)} available</span><span>of {fmtBytes(device.totalBytes)}</span></div><div className="storage-track"><span style={{ width: `${Math.max(2, 100 - device.freeBytes / Math.max(device.totalBytes, 1) * 100)}%` }}/></div></div>
          <button className="toolbar-button eject-button" onClick={eject}><Power size={16}/><span>Eject</span></button>
        </section> : null}
        <section className="content-heading">
          <div><h1>{page === 'library' ? 'All books' : page === 'recent' ? 'Recently added' : device ? 'On your Kindle' : 'Kindle disconnected'}</h1><p>{isDevice ? device ? `${visibleDeviceBooks.length} ${visibleDeviceBooks.length === 1 ? 'book' : 'books'} on this device` : 'Reconnect your Kindle to view its books.' : `${books.length} ${books.length === 1 ? 'book' : 'books'} in your library`}</p></div>
          <div className="view-actions">
            <div className="filter-row" aria-label="Filter books">
              <FilterSelect label="Author" value={authorFilter} values={metadata.authors.values} hasUnknown={metadata.authors.missing} onChange={setAuthorFilter}/>
              <FilterSelect label="Genre" value={genreFilter} values={metadata.genres.values} hasUnknown={metadata.genres.missing} onChange={setGenreFilter}/>
              <FilterSelect label="Language" value={languageFilter} values={metadata.languages.values} hasUnknown={metadata.languages.missing} onChange={setLanguageFilter}/>
              {(authorFilter || genreFilter || languageFilter) && <button className="clear-filters" onClick={() => { setAuthorFilter(''); setGenreFilter(''); setLanguageFilter('') }}>Clear</button>}
            </div>
            {isDevice ? <>{deviceSelection.length > 0 && <button className="toolbar-button danger-button" onClick={() => void removeFromDevice()}><Trash2 size={16}/><span>Remove ({deviceSelection.length})</span></button>}<button className="toolbar-button" onClick={() => navigateTo('library')}><Send size={16}/><span>Add from library</span></button></> : <>
              {selected.length > 0 && <button className="toolbar-button danger-button" onClick={() => void deleteSelected()}><Trash2 size={16}/><span>Remove ({selected.length})</span></button>}
              {selected.length > 0 && device && <button className="toolbar-button" onClick={() => void send(selected)} disabled={transferActive}><Send size={16}/><span>Send to Kindle <em>{selected.length}</em></span></button>}
              <label className="sort-select"><ArrowDownUp size={15}/><select aria-label="Sort books" value={sort} onChange={e => setSort(e.target.value as Sort)}><option value="recent">{isDevice ? 'Device order' : 'Recently added'}</option><option value="title">Title</option><option value="author">Author</option></select></label>
            </>}
            <div className="segmented" role="group" aria-label="View layout"><button aria-label="Grid view" aria-pressed={layout === 'grid'} className={layout === 'grid' ? 'chosen' : ''} onClick={() => setLayout('grid')}><Grid2X2 size={16}/></button><button aria-label="List view" aria-pressed={layout === 'list'} className={layout === 'list' ? 'chosen' : ''} onClick={() => setLayout('list')}><List size={17}/></button></div>
          </div>
        </section>
        <div className={`book-area ${dragging ? 'dragging' : ''}`}>
          {dragging && <div className="drop-overlay"><div><Upload size={27}/><b>Drop EPUBs to add them</b><span>Your originals stay untouched</span></div></div>}
          {isDevice ? device ? <DeviceList deviceBooks={visibleDeviceBooks} layout={layout} hasBooks={deviceBooks.length > 0} searching={!!query.trim() || hasFilters} loading={deviceBooksLoading} error={deviceBooksError} selection={deviceSelection} setSelection={setDeviceSelection} onClearSearch={() => { setQuery(''); setAuthorFilter(''); setGenreFilter(''); setLanguageFilter('') }} onRefresh={() => deviceRefreshRef.current()} onOpen={openDeviceDetails} menu={activeMenu} onMenu={(key, button) => toggleMenu(key, button)} onMenuClose={() => setActiveMenu(null)} onPreview={previewDeviceBook} onRemove={path => { setActiveMenu(null); confirmRemoveFromDevice([path]) }}/> : <DeviceDisconnected onConnect={() => void addDevice()}/> : books.length ? <div className={`book-collection ${layout === 'list' ? 'list-layout' : ''}`}>
            {books.map(book => <BookTile key={book.id} book={book} layout={layout} selected={selected.includes(book.id)} menu={activeMenu?.key === `book:${book.id}` ? activeMenu : null} onMenu={button => toggleMenu(`book:${book.id}`, button)} onMenuClose={() => setActiveMenu(null)} canSend={!!device && !transferActive} sendLabel={device ? `Send to ${device.name}` : 'Connect a Kindle to send'} onPreview={() => { setActiveMenu(null); void openPreview(book) }} onEdit={() => { setActiveMenu(null); void startEdit(book) }} onSend={() => { setActiveMenu(null); void send([book.id]) }} onDelete={() => { setActiveMenu(null); void deleteLocal(book) }} onToggle={e => toggle(book.id, e)} onOpen={() => { setFocusedDeviceBook(null); setFocusedBook(book); setEditing(false) }}/>) }
          </div> : <EmptyState recent={page === 'recent'} searching={!!query || hasFilters} onImport={() => void beginImport()} onClear={() => { setQuery(''); setAuthorFilter(''); setGenreFilter(''); setLanguageFilter('') }}/ >}
        </div>
      </>}
    </main>
    {focusedBook && <div className="details-column">{resizeHandle}<DetailsPanel book={focusedBook} editing={editing} draft={editDraft} setDraft={setEditDraft} onClose={() => { setFocusedBook(null); setEditing(false) }} onEdit={() => void startEdit(focusedBook)} onSave={() => void saveEdit()} onCancel={() => setEditing(false)} onDelete={() => void deleteLocal(focusedBook)} onSend={() => void send([focusedBook.id])} onPreview={() => void openPreview(focusedBook)}/></div>}
    {focusedDeviceBook && <div className="details-column">{resizeHandle}<DeviceDetailsPanel book={focusedDeviceBook} libraryBook={(state?.books ?? []).find(book => book.id === focusedDeviceBook.bookID)} onClose={() => setFocusedDeviceBook(null)} onPreview={book => void openPreview(book)}/></div>}
    {previewTarget && <BookPreviewDialog book={bookPreview?.book ?? previewTarget} preview={bookPreview} loading={previewLoading} error={previewError} chapterIndex={previewChapter} setChapterIndex={setPreviewChapter} onClose={() => { setPreviewTarget(null); setBookPreview(null) }}/>}
    {progress && <TransferToast progress={progress} onCancel={() => void run(() => bridge.cancelTransfer(), () => setProgress(p => p ? { ...p, message: 'Cancellation requested…' } : p))} onClose={() => setProgress(null)}/>}
    {importProgress && <ImportStatus progress={importProgress} onClose={() => setImportProgress(null)}/>}
    {toast && <div className={`toast ${toast.kind === 'error' ? 'toast-error' : ''}`} role={toast.kind === 'error' ? 'alert' : 'status'}>{toast.kind === 'error' ? <AlertCircle size={17}/> : <CheckCircle2 size={17}/>}<span>{toast.text}</span><button onClick={() => setToast(null)} aria-label="Dismiss message"><X size={15}/></button></div>}
    {confirmation && <ConfirmDialog confirmation={confirmation} onCancel={() => setConfirmation(null)} onConfirm={() => { const action = confirmation.action; setConfirmation(null); action() }}/>}
  </div>
}

function BookTile({ book, layout, selected, menu, onMenu, onMenuClose, canSend, sendLabel, onPreview, onEdit, onSend, onDelete, onToggle, onOpen }: { book: Book; layout: 'grid' | 'list'; selected: boolean; menu: MenuAnchor | null; onMenu: (button: HTMLButtonElement) => void; onMenuClose: () => void; canSend: boolean; sendLabel: string; onPreview: () => void; onEdit: () => void; onSend: () => void; onDelete: () => void; onToggle: (e: React.MouseEvent) => void; onOpen: () => void }) {
  const cover = bridge.previewCover(book.coverURL || `/covers/${book.id}`)
  return <article className={`book-tile ${layout === 'list' ? 'book-row' : ''} ${selected ? 'book-selected' : ''}`}>
    <button className="book-main" onClick={onOpen} aria-label={`View ${book.title}`}>
      <div className="cover-wrap"><img src={cover} alt={`${book.title} cover`} draggable="false" onError={e => { e.currentTarget.src = missingCover }}/><span className="cover-gloss"/><span className="quick-check" aria-hidden="true"><Check size={13}/></span></div>
      <span className="book-meta"><strong title={book.title}>{book.title}</strong><span title={book.authors.join(', ')}>{book.authors.join(', ') || 'Unknown author'}</span></span>
    </button>
    <div className="book-menu-anchor" data-book-menu><button className="tile-more" title={`Actions for ${book.title}`} aria-label={`Actions for ${book.title}`} aria-haspopup="menu" aria-expanded={!!menu} onClick={e => { e.stopPropagation(); onMenu(e.currentTarget) }}><MoreHorizontal size={18}/></button></div>
    <button className={`select-hit ${selected ? 'checked' : ''}`} aria-label={`${selected ? 'Deselect' : 'Select'} ${book.title}`} aria-pressed={selected} onClick={onToggle}>{selected && <Check size={12}/>}</button>
    {menu && <ContextMenu anchor={menu} onClose={onMenuClose} items={[{ label: 'Read preview', icon: <BookOpen/>, onSelect: onPreview }, { label: 'Edit details', icon: <Pencil/>, onSelect: onEdit }, { label: deviceMenuLabel(canSend, sendLabel), icon: <Send/>, onSelect: onSend, disabled: !canSend }, { label: 'Remove from library', icon: <Trash2/>, onSelect: onDelete, danger: true }]}/>}
  </article>
}

function deviceMenuLabel(canSend: boolean, label: string) { return canSend || label === 'Connect a Kindle to send' ? label : `${label} · transfer in progress` }

type MenuItem = { label: string; icon: React.ReactNode; onSelect: () => void; disabled?: boolean; danger?: boolean }
function ContextMenu({ anchor, onClose, items }: { anchor: MenuAnchor; onClose: () => void; items: MenuItem[] }) {
  const firstRef = useRef<HTMLButtonElement>(null)
  useEffect(() => { firstRef.current?.focus() }, [])
  function keyboard(e: React.KeyboardEvent<HTMLDivElement>) {
    if (!['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(e.key)) return
    e.preventDefault()
    const options = [...e.currentTarget.querySelectorAll<HTMLButtonElement>('[role="menuitem"]:not([disabled])')]
    const index = options.indexOf(document.activeElement as HTMLButtonElement)
    const next = e.key === 'Home' ? 0 : e.key === 'End' ? options.length - 1 : (index + (e.key === 'ArrowDown' ? 1 : -1) + options.length) % options.length
    options[next]?.focus()
  }
  return createPortal(<div className="book-context-menu" data-book-menu role="menu" aria-label="Book actions" style={{ left: anchor.left, top: anchor.top }} onKeyDown={keyboard}>
    {items.map((item, index) => <button key={item.label} ref={index === 0 ? firstRef : undefined} role="menuitem" disabled={item.disabled} className={item.danger ? 'menu-danger' : ''} onClick={() => { onClose(); item.onSelect() }}><span className="menu-item-icon">{item.icon}</span>{item.label}</button>)}
  </div>, document.body)
}

function KindleIcon() { return <svg viewBox="0 0 20 20" aria-hidden="true"><rect x="3.5" y="1.75" width="13" height="16.5" rx="2.4" fill="none" stroke="currentColor" strokeWidth="1.45"/><path d="M5.7 4h8.6v10.2H5.7z" fill="none" stroke="currentColor" strokeWidth="1.1"/><circle cx="10" cy="16.1" r=".65" fill="currentColor"/></svg> }

const unknownFilter = '__unknown__'
function matchesFilter(values: string[] | undefined, selected: string) {
  if (!selected) return true
  const present = (values ?? []).some(value => value.trim())
  return selected === unknownFilter ? !present : (values ?? []).includes(selected)
}
function FilterSelect({ label, value, values, hasUnknown, onChange }: { label: string; value: string; values: string[]; hasUnknown: boolean; onChange: (value: string) => void }) {
  const plural = label === 'Genre' ? 'genres' : `${label.toLowerCase()}s`
  return <label className="filter-select"><span>{label}</span><select aria-label={`Filter by ${label.toLowerCase()}`} value={value} onChange={e => onChange(e.target.value)}>
    <option value="">All {plural}</option>{values.map(item => <option value={item} key={item}>{label === 'Language' ? languageLabel(item) : item}</option>)}{hasUnknown && <option value={unknownFilter}>Unknown {label.toLowerCase()}</option>}
  </select></label>
}
function languageLabel(code: string) {
  try { return new Intl.DisplayNames([navigator.language || 'en'], { type: 'language' }).of(code) || code }
  catch { return code }
}

function BookPreviewDialog({ book, preview, loading, error, chapterIndex, setChapterIndex, onClose }: { book: Book; preview: BookPreview | null; loading: boolean; error: string; chapterIndex: number; setChapterIndex: (index: number) => void; onClose: () => void }) {
  const chapters = preview?.chapters ?? []
  const active = chapters[chapterIndex]
  const safeHref = active?.href.startsWith('/preview/') && !active.href.startsWith('//') ? active.href : ''
  const previewHtml = bridge.preview ? `<!doctype html><html><head><meta name="viewport" content="width=device-width, initial-scale=1"><style>body{font:17px/1.8 Georgia,serif;max-width:620px;margin:50px auto;padding:0 28px;color:#34343a}h1{font:600 27px/1.25 Georgia,serif}p{margin:1.1em 0}</style></head><body><h1>${escapeHTML(active?.title ?? 'A quiet beginning')}</h1><p>The rain had stopped just before the first light touched the windows. In the stillness, the old house seemed to be listening.</p><p>${escapeHTML(book.title)} waited on the table, its pages holding a map to somewhere the narrator had not yet learned to name.</p><p>Outside, a bird called once from the garden. Then the day began.</p></body></html>` : undefined
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])
  return <div className="preview-backdrop" onMouseDown={e => { if (e.target === e.currentTarget) onClose() }}>
    <section className="preview-dialog" role="dialog" aria-modal="true" aria-label={`Preview ${book.title}`}>
      <header className="preview-header"><div><div className="eyebrow">BOOK PREVIEW</div><h2>{book.title}</h2><span>{book.authors.join(', ') || 'Unknown author'} · {book.language ? languageLabel(book.language) : 'Language unknown'}</span></div><button className="icon-button" onClick={onClose} aria-label="Close preview"><X size={18}/></button></header>
      <div className="preview-layout"><nav className="chapter-nav" aria-label="Chapters"><strong>Contents</strong>{chapters.map((chapter, index) => <button key={chapter.id} className={index === chapterIndex ? 'active' : ''} onClick={() => setChapterIndex(index)}>{chapter.title || `Chapter ${index + 1}`}</button>)}</nav>
        <div className="preview-reader">{loading ? <div className="preview-message"><LoaderCircle className="spinning"/>Loading preview…</div> : error ? <div className="preview-message preview-error"><AlertCircle/>{error}</div> : !active ? <div className="preview-message">No preview chapters are available.</div> : <div className="preview-page"><iframe title={`${book.title}: ${active.title}`} sandbox="allow-same-origin" referrerPolicy="no-referrer" src={bridge.preview ? undefined : safeHref || undefined} srcDoc={previewHtml} /></div>}</div>
      </div>
    </section>
  </div>
}
function escapeHTML(value: string) { return value.replace(/[&<>"']/g, char => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[char]!) ) }

function DeviceDisconnected({ onConnect }: { onConnect: () => void }) {
  return <div className="empty-device"><div className="empty-art"><HardDrive size={35}/><span>×</span></div><h2>Kindle disconnected</h2><p>Reconnect your Kindle, then add it here to browse or manage its books.</p><button className="toolbar-button" onClick={onConnect}><Plus size={16}/> Connect a Kindle</button></div>
}

function DeviceList({ deviceBooks, layout, hasBooks, searching, loading, error, selection, setSelection, onClearSearch, onRefresh, onOpen, menu, onMenu, onMenuClose, onPreview, onRemove }: { deviceBooks: DeviceBook[]; layout: 'grid' | 'list'; hasBooks: boolean; searching: boolean; loading: boolean; error: string; selection: string[]; setSelection: React.Dispatch<React.SetStateAction<string[]>>; onClearSearch: () => void; onRefresh: () => void; onOpen: (book: DeviceBook) => void; menu: MenuAnchor | null; onMenu: (key: string, button: HTMLButtonElement) => void; onMenuClose: () => void; onPreview: (bookID: string) => void; onRemove: (path: string) => void }) {
  if (!deviceBooks.length && loading) return <div className="empty-device" role="status" aria-live="polite"><div className="empty-art"><LoaderCircle className="spinning" size={32}/></div><h2>Reading Kindle contents…</h2><p>Looking for books on the connected device.</p></div>
  if (!deviceBooks.length && !hasBooks && error) return <div className="empty-device"><div className="empty-art"><AlertCircle size={32}/></div><h2>Couldn’t read Kindle contents</h2><p>{error}</p><button className="toolbar-button" onClick={onRefresh}><RefreshCw size={16}/> Try again</button></div>
  return <>{error && <div className="device-load-error" role="status"><AlertCircle size={15}/><span>Couldn’t refresh Kindle contents. Showing the last loaded books.</span><button className="quiet-button" onClick={onRefresh}>Try again</button></div>}{!deviceBooks.length ? <div className="empty-device"><div className="empty-art"><HardDrive size={35}/><span>· · ·</span></div><h2>{searching && hasBooks ? 'No Kindle books match these filters' : 'No books found on this Kindle'}</h2><p>{searching && hasBooks ? 'Try another search or clear your filters.' : 'Books in the device’s documents folder will appear here.'}</p>{searching && hasBooks ? <button className="quiet-button" onClick={onClearSearch}>Clear filters</button> : <button className="toolbar-button" onClick={onRefresh}><RefreshCw size={16}/> Refresh device</button>}</div> : layout === 'grid' ? <div className="book-collection device-collection">{deviceBooks.map(book => <DeviceTile key={book.path} book={book} selected={selection.includes(book.path)} menu={menu?.key === `device:${book.path}` ? menu : null} onMenu={button => onMenu(`device:${book.path}`, button)} onMenuClose={onMenuClose} onPreview={() => onPreview(book.bookID)} onRemove={() => onRemove(book.path)} onToggle={() => setSelection(s => s.includes(book.path) ? s.filter(p => p !== book.path) : [...s, book.path])} onOpen={() => onOpen(book)}/>)}</div> : <div className="device-list" role="list">
    {deviceBooks.map(book => <div role="listitem" className={`device-book ${selection.includes(book.path) ? 'row-selected' : ''}`} key={book.path}>
      <button className={`device-check ${selection.includes(book.path) ? 'checked' : ''}`} onClick={() => setSelection(s => s.includes(book.path) ? s.filter(p => p !== book.path) : [...s, book.path])} aria-label={`Select ${book.title || fileTitle(book.name)}`} aria-pressed={selection.includes(book.path)}>{selection.includes(book.path) && <Check size={13}/>}</button>
      <div className="device-book-icon"><BookOpen size={17}/></div><div className="device-book-copy"><button onClick={() => onOpen(book)}>{book.title || fileTitle(book.name)}</button><span>{extensionFromPath(book.path)} · {fmtBytes(book.size)}</span></div><span className="device-book-status">On Kindle</span><div className="book-menu-anchor" data-book-menu><button className="tile-more" aria-label={`Actions for ${book.title || fileTitle(book.name)}`} aria-haspopup="menu" aria-expanded={menu?.key === `device:${book.path}`} onClick={e => { e.stopPropagation(); onMenu(`device:${book.path}`, e.currentTarget) }}><MoreHorizontal size={18}/></button></div>
      {menu?.key === `device:${book.path}` && <ContextMenu anchor={menu} onClose={onMenuClose} items={[{ label: 'View details', icon: <BookOpen/>, onSelect: () => onOpen(book) }, { label: 'Read preview', icon: <BookOpen/>, onSelect: () => onPreview(book.bookID), disabled: !book.bookID }, { label: 'Remove from Kindle', icon: <Trash2/>, onSelect: () => onRemove(book.path), danger: true }]}/>}
    </div>)}
  </div>}</>
}

function extensionFromPath(path: string) { return path.split(/[\\/]/).pop()?.match(/\.([^.]+)$/)?.[1]?.toUpperCase() ?? 'BOOK' }
function fileTitle(name: string) { return name.replace(/\.(mobi|azw|azw3|prc|epub)$/i, '') }

function DeviceTile({ book, selected, menu, onMenu, onMenuClose, onPreview, onRemove, onToggle, onOpen }: { book: DeviceBook; selected: boolean; menu: MenuAnchor | null; onMenu: (button: HTMLButtonElement) => void; onMenuClose: () => void; onPreview: () => void; onRemove: () => void; onToggle: () => void; onOpen: () => void }) {
  const title = book.title || fileTitle(book.name)
  const cover = book.coverURL ? bridge.previewCover(book.coverURL) : missingCover
  return <article className={`book-tile device-tile ${selected ? 'book-selected' : ''}`}>
    <button className="book-main" onClick={onOpen} aria-label={`View ${title}`}><div className="cover-wrap"><img src={cover} alt={`${title} cover`} onError={e => { e.currentTarget.src = missingCover }}/><span className="cover-gloss"/></div>
    <span className="book-meta"><strong title={title}>{title}</strong><span title={(book.authors ?? []).join(', ')}>{book.authors?.join(', ') || 'Unknown author'}</span></span></button>
    <div className="book-menu-anchor" data-book-menu><button className="tile-more" aria-label={`Actions for ${title}`} aria-haspopup="menu" aria-expanded={!!menu} onClick={e => { e.stopPropagation(); onMenu(e.currentTarget) }}><MoreHorizontal size={18}/></button></div>
    <button className={`select-hit ${selected ? 'checked' : ''}`} aria-label={`${selected ? 'Deselect' : 'Select'} ${title}`} aria-pressed={selected} onClick={onToggle}>{selected && <Check size={12}/>}</button>
    {menu && <ContextMenu anchor={menu} onClose={onMenuClose} items={[{ label: 'View details', icon: <BookOpen/>, onSelect: onOpen }, { label: 'Read preview', icon: <BookOpen/>, onSelect: onPreview, disabled: !book.bookID }, { label: 'Remove from Kindle', icon: <Trash2/>, onSelect: onRemove, danger: true }]}/>}
  </article>
}

function DeviceDetailsPanel({ book, libraryBook, onClose, onPreview }: { book: DeviceBook; libraryBook?: Book; onClose: () => void; onPreview: (book: Book) => void }) {
  const title = book.title || fileTitle(book.name)
  const cover = book.coverURL ? bridge.previewCover(book.coverURL) : missingCover
  return <aside className="details-panel device-details-panel" aria-label="Device book details"><div className="details-top"><span>ON KINDLE</span><button className="icon-button" onClick={onClose} aria-label="Close book details"><PanelRightClose size={18}/></button></div>
    <div className="detail-cover"><img src={cover} alt={`${title} cover`} onError={e => { e.currentTarget.src = missingCover }}/></div><h2 className="detail-title">{title}</h2><p className="detail-author">{book.authors?.join(', ') || 'Unknown author'}</p>
    <div className="details-actions">{libraryBook ? <button className="toolbar-button primary-button" onClick={() => onPreview(libraryBook)}><BookOpen size={15}/> Read preview</button> : <p className="device-preview-unavailable">A preview is available only for books in this library.</p>}</div>
    <MetadataDetails metadata={book} format={extensionFromPath(book.path)} fileSize={book.size}/>
  </aside>
}

function ResizeHandle({ width, onWidthChange }: { width: number; onWidthChange: (width: number) => void }) {
  const dragging = useRef(false)
  const setFromPointer = (clientX: number) => onWidthChange(Math.max(240, Math.min(480, window.innerWidth - 12 - clientX)))
  return <button className="detail-resize-handle" role="separator" aria-label="Resize book details panel" aria-orientation="vertical" aria-valuemin={240} aria-valuemax={480} aria-valuenow={width} tabIndex={0}
    onPointerDown={e => { e.preventDefault(); e.currentTarget.focus(); dragging.current = true; e.currentTarget.setPointerCapture(e.pointerId) }}
    onPointerMove={e => { if (dragging.current) setFromPointer(e.clientX) }}
    onPointerUp={e => { dragging.current = false; if (e.currentTarget.hasPointerCapture(e.pointerId)) e.currentTarget.releasePointerCapture(e.pointerId) }}
    onPointerCancel={() => { dragging.current = false }}
    onKeyDown={e => { if (e.key === 'ArrowLeft') { e.preventDefault(); onWidthChange(Math.min(480, width + 16)) } else if (e.key === 'ArrowRight') { e.preventDefault(); onWidthChange(Math.max(240, width - 16)) } }} />
}

type MetadataSource = Pick<DeviceBook, 'publisher' | 'published' | 'identifiers' | 'description' | 'series' | 'seriesIndex' | 'authorSort' | 'titleSort' | 'pageCount' | 'addedAt' | 'modifiedAt' | 'language' | 'subjects'>
function MetadataDetails({ metadata, format, fileSize }: { metadata: MetadataSource; format: string; fileSize?: number }) {
  const fields: [string, string][] = []
  if (metadata.publisher?.trim()) fields.push(['Publisher', metadata.publisher.trim()])
  if (metadata.published?.trim()) {
    const published = metadata.published.trim()
    fields.push(['Published', /^\d{4}(?:-\d{2}(?:-\d{2}(?:T.*)?)?)?$/.test(published) ? dateLabel(published) : published])
  }
  if (metadata.series?.trim()) fields.push(['Series', `${metadata.series.trim()}${metadata.seriesIndex?.trim() ? ` · ${metadata.seriesIndex.trim()}` : ''}`])
  if (metadata.pageCount && metadata.pageCount > 0) fields.push(['Pages', metadata.pageCount.toLocaleString()])
  if (metadata.language?.trim()) fields.push(['Language', languageLabel(metadata.language)])
  if (metadata.subjects?.length) fields.push(['Genres', metadata.subjects.filter(Boolean).join(', ')])
  const authorSort = metadata.authorSort?.map(value => value.trim()).filter(Boolean) ?? []
  if (authorSort.length) fields.push(['Author sort', authorSort.join(' · ')])
  if (metadata.titleSort?.trim()) fields.push(['Title sort', metadata.titleSort.trim()])
  if (format) fields.push(['Format', format])
  if (fileSize !== undefined && fileSize > 0) fields.push(['File size', fmtBytes(fileSize)])
  if (metadata.addedAt) fields.push(['Added', dateLabel(metadata.addedAt)])
  if (metadata.modifiedAt) fields.push(['Modified', dateLabel(metadata.modifiedAt)])
  const identifiers = Object.entries(metadata.identifiers ?? {}).filter(([key, value]) => key.trim() && value.trim())
  if (!fields.length && !identifiers.length && !metadata.description?.trim()) return null
  return <div className="metadata-details">
    {fields.map(([label, value]) => <div className="metadata-row" key={label}><span>{label}</span><b title={value}>{value}</b></div>)}
    {!!identifiers.length && <section className="metadata-section"><h3>Identifiers</h3>{identifiers.map(([key, value]) => <div className="metadata-row" key={key}><span>{key.toUpperCase()}</span><b title={value}>{value}</b></div>)}</section>}
    {!!metadata.description?.trim() && <section className="metadata-section"><h3>Description</h3><p className="metadata-description">{metadata.description.trim()}</p></section>}
  </div>
}
function dateLabel(value: string) {
  const year = value.match(/^(\d{4})$/)
  if (year) return year[1]
  const month = value.match(/^(\d{4})-(\d{2})$/)
  if (month) { const date = new Date(Date.UTC(Number(month[1]), Number(month[2]) - 1, 1)); return date.toLocaleDateString(undefined, { year: 'numeric', month: 'long', timeZone: 'UTC' }) }
  const dateOnly = value.match(/^(\d{4})-(\d{2})-(\d{2})$/)
  const date = dateOnly ? new Date(Date.UTC(Number(dateOnly[1]), Number(dateOnly[2]) - 1, Number(dateOnly[3]))) : new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric', timeZone: dateOnly ? 'UTC' : undefined })
}

function EmptyState({ recent, searching, onImport, onClear }: { recent: boolean; searching: boolean; onImport: () => void; onClear: () => void }) {
  return <div className="empty-state"><div className="empty-art"><div className="empty-book book-a"/><div className="empty-book book-b"/><div className="empty-book book-c"/><span className="sparkle s1">✳</span><span className="sparkle s2">✦</span></div><h2>{searching ? 'No books match that search' : recent ? 'Nothing new just yet' : 'Attune your library'}</h2><p>{searching ? 'Try a different title or author.' : recent ? 'Books you add will show up here.' : 'Bring your EPUBs together in one lovely little library.'}</p>{searching ? <button className="quiet-button" onClick={onClear}>Clear search</button> : !recent && <><button className="toolbar-button primary-button" onClick={onImport}><Plus size={17}/> Import EPUBs</button><span className="drop-hint">or drop EPUB files anywhere in this window</span></>}</div>
}

const missingCover = `data:image/svg+xml,${encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 420 620"><rect width="420" height="620" fill="#e6e6e8"/><path d="M145 230c0-12 10-22 22-22h80c12 0 22 10 22 22v150c0 12-10 22-22 22h-80c-12 0-22-10-22-22z" fill="#c8c9cd"/><path d="M210 208v194" stroke="#eee" stroke-width="7"/><path d="M163 263h38m-38 22h32m-32 22h36m38-44h28m-28 22h28m-28 22h28" stroke="#eee" stroke-width="5" stroke-linecap="round"/></svg>')}`
function DetailsPanel({ book, editing, draft, setDraft, onClose, onEdit, onSave, onCancel, onDelete, onSend, onPreview }: { book: Book; editing: boolean; draft: EditDraft; setDraft: React.Dispatch<React.SetStateAction<EditDraft>>; onClose: () => void; onEdit: () => void; onSave: () => void; onCancel: () => void; onDelete: () => void; onSend: () => void; onPreview: () => void }) {
  const update = (key: keyof EditDraft, value: string) => setDraft(current => ({ ...current, [key]: value }))
  return <aside className="details-panel" aria-label="Book details">
    <div className="details-top"><span>BOOK DETAILS</span><button className="icon-button" onClick={onClose} aria-label="Close book details"><PanelRightClose size={18}/></button></div>
    <div className="detail-cover"><img src={bridge.previewCover(book.coverURL || `/covers/${book.id}`)} alt={`${book.title} cover`} onError={e => { e.currentTarget.src = missingCover }}/></div>
    {editing ? <div className="edit-fields">
      <label>Title<input value={draft.title} autoFocus onChange={e => update('title', e.target.value)}/></label>
      <label>Authors <span className="field-hint">separate with semicolons</span><input value={draft.authors} onChange={e => update('authors', e.target.value)}/></label>
      <label>Publisher<input value={draft.publisher} onChange={e => update('publisher', e.target.value)}/></label>
      <label>Language <span className="field-hint">language code, e.g. en or en-GB</span><input value={draft.language} onChange={e => update('language', e.target.value)}/></label>
      <label>Publication date <span className="field-hint">year, month, or full date</span><input value={draft.published} placeholder="YYYY-MM-DD" onChange={e => update('published', e.target.value)}/></label>
      <label>Genres <span className="field-hint">separate with semicolons</span><input value={draft.subjects} onChange={e => update('subjects', e.target.value)}/></label>
      <div className="edit-series"><label>Series<input value={draft.series} onChange={e => update('series', e.target.value)}/></label><label>Index<input value={draft.seriesIndex} onChange={e => update('seriesIndex', e.target.value)}/></label></div>
      <label>Description<textarea rows={5} value={draft.description} onChange={e => update('description', e.target.value)}/></label>
      <div className="edit-actions"><button className="quiet-button" onClick={onCancel}>Cancel</button><button className="toolbar-button primary-button" onClick={onSave} disabled={!draft.title.trim()}><Check size={15}/> Save</button></div>
    </div> : <><h2 className="detail-title">{book.title}</h2><p className="detail-author">{book.authors.join(', ') || 'Unknown author'}</p><div className="details-actions"><button className="toolbar-button primary-button" onClick={onPreview}><BookOpen size={15}/> Read preview</button><button className="toolbar-button" onClick={onSend}><Send size={15}/> Send to Kindle</button><button className="toolbar-button" onClick={onEdit}><Pencil size={15}/> Edit details</button><button className="toolbar-button danger-button" onClick={onDelete}><Trash2 size={15}/> Remove from library</button></div><MetadataDetails metadata={book} format="EPUB" fileSize={book.fileSize}/></>}
    <div className="detail-footer"><span>Original EPUB is safely stored on this Mac.</span><CircleHelp size={15}/></div>
  </aside>
}

function TransferToast({ progress, onCancel, onClose }: { progress: Progress; onCancel: () => void; onClose: () => void }) {
  const done = progress.stage === 'done' && (!progress.total || progress.current >= progress.total)
  const terminal = done || ['error', 'cancelled'].includes(progress.stage)
  return <div className={`transfer-toast ${progress.stage === 'error' ? 'transfer-error' : ''}`} role="status"><div className="transfer-icon">{done ? <CheckCircle2 size={19}/> : progress.stage === 'error' ? <AlertCircle size={19}/> : progress.stage === 'cancelled' ? <X size={19}/> : <LoaderCircle className="spinning" size={19}/>}</div><div className="transfer-copy"><div className="transfer-title">{done ? 'Transfer complete' : progress.stage === 'error' ? 'Transfer needs attention' : progress.stage === 'cancelled' ? 'Transfer cancelled' : progress.title || 'Sending books to Kindle'}</div><div className="transfer-subtitle">{progress.message || stageName(progress.stage)}</div><div className="progress-track"><span style={{ width: `${Math.max(2, Math.min(100, progress.total ? progress.current / progress.total * 100 : 9))}%` }}/></div></div>{terminal ? <button className="icon-button" onClick={onClose} aria-label="Close transfer status"><X size={16}/></button> : <button className="quiet-button cancel-transfer" onClick={onCancel}>Cancel</button>}</div>
}
function ImportStatus({ progress, onClose }: { progress: ImportProgress; onClose: () => void }) {
  const final = progress.stage !== 'importing'
  const percent = progress.total ? Math.min(100, Math.round(progress.current / progress.total * 100)) : 0
  const summary = `${progress.imported} imported · ${progress.duplicates} duplicate${progress.duplicates === 1 ? '' : 's'} · ${progress.failed} failed`
  return <section className={`import-status ${progress.stage === 'error' ? 'import-status-error' : ''}`} role="status" aria-live="polite" aria-atomic="false">
    <div className="import-status-icon">{progress.stage === 'error' ? <AlertCircle size={18}/> : final ? <CheckCircle2 size={18}/> : <LoaderCircle size={18} className="spinning"/>}</div>
    <div className="import-status-body">
      <div className="import-status-heading"><strong>{progress.stage === 'error' ? 'Import stopped' : final ? 'Import complete' : progress.total ? `Importing books · ${progress.current} of ${progress.total} complete` : 'Choose EPUBs to import'}</strong>{final && <button className="icon-button" onClick={onClose} aria-label="Dismiss import summary"><X size={16}/></button>}</div>
      <div className="import-filename" title={progress.message || progress.filename}>{progress.message || progress.filename}</div>
      {progress.total > 0 && <div className="progress-track" role="progressbar" aria-label="Import progress" aria-valuemin={0} aria-valuemax={progress.total} aria-valuenow={Math.min(progress.current, progress.total)}><span style={{ width: `${percent}%` }}/></div>}
      <div className="import-counts">{summary}</div>
    </div>
  </section>
}
function ConfirmDialog({ confirmation, onCancel, onConfirm }: { confirmation: Confirmation; onCancel: () => void; onConfirm: () => void }) {
  const cancelRef = useRef<HTMLButtonElement>(null)
  useEffect(() => { cancelRef.current?.focus() }, [])
  function trapFocus(e: React.KeyboardEvent<HTMLElement>) {
    if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); onCancel(); return }
    if (e.key !== 'Tab') return
    const buttons = e.currentTarget.querySelectorAll<HTMLButtonElement>('button:not([disabled])')
    if (e.shiftKey && document.activeElement === buttons[0]) { e.preventDefault(); buttons[buttons.length - 1]?.focus() }
    else if (!e.shiftKey && document.activeElement === buttons[buttons.length - 1]) { e.preventDefault(); buttons[0]?.focus() }
  }
  return <div className="confirm-backdrop" onMouseDown={e => { if (e.target === e.currentTarget) onCancel() }}>
    <section className="confirm-dialog" role="alertdialog" aria-modal="true" aria-labelledby="confirm-title" aria-describedby="confirm-description" onKeyDown={trapFocus}>
      <h2 id="confirm-title">{confirmation.title}</h2><p id="confirm-description">{confirmation.description}</p>
      <div className="confirm-actions"><button ref={cancelRef} className="toolbar-button" onClick={onCancel}>Cancel</button><button className="toolbar-button danger-button confirm-action" onClick={onConfirm}>{confirmation.confirmLabel}</button></div>
    </section>
  </div>
}
function Settings({ state, onOpenFolder }: { state: AppState | null; onOpenFolder: () => void }) {
  return <section className="settings-page"><div className="settings-heading"><div className="settings-icon"><Settings2 size={22}/></div><div><h1>Settings</h1><p>A few details about your library.</p></div></div><div className="settings-card"><div><strong>Library location</strong><span>{state?.libraryPath || 'Unavailable'}</span></div><button className="toolbar-button" onClick={onOpenFolder}><FolderOpen size={16}/> Open folder</button></div><div className="settings-card"><div><strong>EPUB converter</strong><span>{state?.converterReady ? `Ready · ${state.converterVersion || 'installed'}` : 'Not available yet'}</span></div><span className={`converter-status ${state?.converterReady ? 'ready' : ''}`}>{state?.converterReady ? 'Ready' : 'Unavailable'}</span></div><div className="settings-note"><CircleHelp size={16}/><span>Attune keeps your book files on this Mac. No cloud account is used.</span></div></section>
}
function stageName(stage: string) { return ({ queued: 'Waiting in queue', converting: 'Converting for Kindle', transferring: 'Copying to device', done: 'Saved to Kindle', cancelled: 'Transfer cancelled', error: 'Check your device and try again' } as Record<string, string>)[stage] ?? 'Preparing transfer' }
function message(e: unknown) { return e instanceof Error ? e.message : String(e || 'Something went wrong. Try again.') }
