import { useCallback, useEffect, useMemo, useRef, useState, type CSSProperties } from 'react'
import { createPortal } from 'react-dom'
import {
  BookOpen, LibraryBig, Clock3, HardDrive, Settings2, Search, Plus, Grid2X2, List, ArrowDownUp, ArrowLeft, ArrowRight,
  MoreHorizontal, X, Pencil, Trash2, Send, RefreshCw, Power, Check, AlertCircle,
  CircleHelp, FolderOpen, Upload, Download, LoaderCircle, PanelRightClose, ImagePlus,
  Maximize2, Minimize2, PanelLeftClose, PanelLeftOpen, Type, ChevronDown, ChevronLeft, ChevronRight,
  Bookmark, BookmarkPlus, Columns2, AlignLeft, Trash,
} from 'lucide-react'
import { bridge, progressFromEvent } from './bridge'
import type { ReaderAnnotation } from './bridge'
import type { AppState, Book, BookMetadataEdit, BookPreview, Device, DeviceBook, DevicePreferences, Progress, ImportProgress, ImportResult } from './types'
import { useTextSize, useBookSize, type TextSize } from './appearance'
import BookSizeControl from './GallerySize'
import ActivityCenter, { useActivityLog } from './ActivityCenter'
import type { ActivityEntry } from './ActivityCenter'
import { InlineBanner, useNotifications } from './Notifications'
import Updates, { useUpdater, UpdateSettingsCard } from './Updates'
import type { UpdateState } from './types'
import DropdownSelect from './DropdownSelect'
import Welcome from './Welcome'
import type { CalibreLocation } from './Welcome'
import { createReaderPagination, type ReaderPagination } from './readerPagination'
import { ReaderSurface, type ReaderSurfaceHandle } from './readers/ReaderSurface'
import type { ReaderSection, ReaderSettings } from './readers/adapter'

type Page = 'library' | 'recent' | 'device' | 'updates' | 'settings'
type Sort = 'recent' | 'title' | 'author'
type Confirmation = { title: string; description: string; confirmLabel: string; cancelLabel?: string; danger?: boolean; action: () => void }
type EditDraft = { title: string; authors: string; publisher: string; language: string; published: string; subjects: string; series: string; seriesIndex: string; description: string }
type MenuAnchor = { key: string; left: number; top: number }
type IntakeRequest = { paths: string[]; names: string[]; device: { id: string; name: string } | null }
type PasteMenu = { left: number; top: number } | null
type DevicePreferencesDraft = DevicePreferences & { deviceID: string; deviceName: string; foldersText: string; requiresExplicitSelection: boolean }
const deviceFormatOptions = ['epub', 'pdf', 'azw3', 'mobi', 'prc', 'azw', 'fb2', 'txt', 'cbz', 'cbr', 'rtf', 'doc', 'docx', 'odt', 'html', 'htmlz', 'pdb', 'lit', 'zip']
const fmtBytes = (n: number) => n < 1_000 ? `${n} B` : n < 1_000_000 ? `${(n / 1_000).toFixed(0)} KB` : n < 1_000_000_000 ? `${(n / 1_000_000).toFixed(1)} MB` : `${(n / 1_000_000_000).toFixed(1)} GB`
function bookCover(book: Book) {
  const url = bridge.previewCover(book.coverURL || `/covers/${book.id}`)
  return bridge.preview ? url : `${url}${url.includes('?') ? '&' : '?'}revision=${book.revision}`
}
const recentWindowMs = 30 * 86400000
const readerFormatOrder = ['epub', 'pdf', 'mobi', 'azw3', 'azw', 'fb2', 'txt', 'cbz', 'cbr']
function localDateKey(date: Date) { return `${date.getFullYear()}-${date.getMonth()}-${date.getDate()}` }
function recentDayLabel(date: Date, today: Date, yesterday: Date) {
  const fullDate = new Intl.DateTimeFormat(undefined, { weekday: 'long', year: 'numeric', month: 'long', day: 'numeric' }).format(date)
  const key = localDateKey(date)
  const relative = key === localDateKey(today) ? 'Today' : key === localDateKey(yesterday) ? 'Yesterday' : ''
  return relative ? `${relative} · ${fullDate}` : fullDate
}
function formatAddedTime(timestamp: number) {
  if (!Number.isFinite(timestamp)) return undefined
  const date = new Date(timestamp)
  return {
    time: new Intl.DateTimeFormat(undefined, { hour: 'numeric', minute: '2-digit' }).format(date),
    dateTime: date.toISOString(),
    title: new Intl.DateTimeFormat(undefined, { dateStyle: 'full', timeStyle: 'long' }).format(date),
  }
}

export default function App() {
  const updater = useUpdater()
  const [state, setState] = useState<AppState | null>(null)
  const [onboarding, setOnboarding] = useState<{ required: boolean; libraries: CalibreLocation[] } | null>(null)
  const [onboardingError, setOnboardingError] = useState('')
  const onboardingReady = useRef(false)
  const [textSize, setTextSize] = useTextSize()
  const [bookSize, setBookSize] = useBookSize()
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
  const [detailWidth, setDetailWidth] = useState(() => Math.min(400, Math.max(300, Math.round(window.innerWidth * 0.27))))
  const [deviceBooks, setDeviceBooks] = useState<DeviceBook[]>([])
  const [deviceBooksLoading, setDeviceBooksLoading] = useState(false)
  const [deviceBooksError, setDeviceBooksError] = useState('')
  const [deviceSelection, setDeviceSelection] = useState<string[]>([])
  const [activeDeviceID, setActiveDeviceID] = useState(() => localStorage.getItem('attune-active-device') ?? '')
  const [devicePreferencesDraft, setDevicePreferencesDraft] = useState<DevicePreferencesDraft | null>(null)
  const [importReport, setImportReport] = useState<ImportResult | null>(null)
  const [ejectingDevice, setEjectingDevice] = useState<Device | null>(null)
  const [confirmation, setConfirmation] = useState<Confirmation | null>(null)
  const [previewTarget, setPreviewTarget] = useState<Book | null>(null)
  const [readerOpen, setReaderOpen] = useState(false)
  const [bookPreview, setBookPreview] = useState<BookPreview | null>(null)
  const [previewLoading, setPreviewLoading] = useState(false)
  const [previewError, setPreviewError] = useState('')
  const [previewChapter, setPreviewChapter] = useState(0)
  const [readerSearch, setReaderSearch] = useState('')
  const readerFindRef = useRef<() => void>(() => {})
  const readerTurnRef = useRef<(direction: -1 | 1) => void>(() => {})
  const readerSearchRef = useRef<HTMLInputElement>(null)
  const [progress, setProgress] = useState<Progress | null>(null)
  const { notify, dismiss } = useNotifications()
  const [busy, setBusy] = useState(false)
  const [changingLibrary, setChangingLibrary] = useState(false)
  const [dragging, setDragging] = useState(false)
  const [editing, setEditing] = useState(false)
  const [editDraft, setEditDraft] = useState<EditDraft>({ title: '', authors: '', publisher: '', language: '', published: '', subjects: '', series: '', seriesIndex: '', description: '' })
  const [activeMenu, setActiveMenu] = useState<MenuAnchor | null>(null)
  const [intake, setIntake] = useState<IntakeRequest | null>(null)
  const [pasteMenu, setPasteMenu] = useState<PasteMenu>(null)
  const { entries, begin, update: updateActivity, remove: removeActivity } = useActivityLog()
  const updateActivityID = useRef<string | null>(null)
  const searchRef = useRef<HTMLInputElement>(null)
  const coverInputRef = useRef<HTMLInputElement>(null)
  const coverTarget = useRef<Book | null>(null)
  const importGuard = useRef(false)
  const transferGuard = useRef(false)
  const refreshGuard = useRef(false)
  const libraryChangeGuard = useRef(false)
  const busyRef = useRef(false)
  const importPathsRef = useRef<(paths: string[], target?: 'library' | 'device') => void>(() => {})
  const pasteFilesRef = useRef<() => void>(() => {})
  const importActivityId = useRef<string | null>(null)
  const transferActivityId = useRef<string | null>(null)
  const pasteHandledAt = useRef(0)
  const clipboardReadInFlight = useRef(false)
  const intakeFlowGuard = useRef(false)
  const deviceRefreshRef = useRef<() => void>(() => {})
  const deviceBooksOwner = useRef<string | null>(null)
  const hiddenEjectingDevices = useRef(new Set<string>())
  const ejectingDeviceIDs = useRef(new Set<string>())
  busyRef.current = busy

  function navigateTo(nextPage: Page) {
    setReaderOpen(false)
    setNavigation(current => {
      if (current.pages[current.index] === nextPage) return current
      const pages = [...current.pages.slice(0, current.index + 1), nextPage]
      return { pages, index: pages.length - 1 }
    })
  }

  const refresh = useCallback(async (force = false) => {
    if (!onboardingReady.current) return
    if (refreshGuard.current || (importGuard.current && !force)) return
    refreshGuard.current = true
    try {
      const next = await bridge.getState()
      const found = new Set(next.devices.map(device => device.id))
      for (const id of hiddenEjectingDevices.current) if (!found.has(id)) hiddenEjectingDevices.current.delete(id)
      setState({ ...next, devices: next.devices.filter(device => !hiddenEjectingDevices.current.has(device.id) && !ejectingDeviceIDs.current.has(device.id)) })
    }
    catch (e) { notify({ text: message(e), kind: 'error' }) }
    finally { refreshGuard.current = false }
  }, [])
  useEffect(() => {
    const next = updater.state
    if (next.status === 'downloading' && !updateActivityID.current) updateActivityID.current = begin('update', 'Updating Attune')
    const id = updateActivityID.current
    if (!id) return
    const status = next.downloaded ? 'success' : next.status === 'error' ? 'error' : 'running'
    updateActivity(id, { status, current: next.progress || 0, total: 100, detail: next.downloaded ? 'Downloaded and verified · ready to restart' : next.error || `Downloading ${next.latestVersion}` })
    if (status !== 'running') updateActivityID.current = null
  }, [updater.state, begin, updateActivity])
  const loadOnboarding = useCallback(async () => {
    onboardingReady.current = false
    setOnboarding(null)
    setOnboardingError('')
    try {
      const next = await bridge.getOnboardingState()
      setOnboarding(next)
      onboardingReady.current = !next.required
      if (!next.required) await refresh()
    } catch (e) {
      setOnboardingError(`Attune couldn’t check whether setup is complete. ${message(e)}`)
    }
  }, [refresh])
  useEffect(() => { void loadOnboarding() }, [loadOnboarding])
  useEffect(() => {
    const timer = window.setInterval(() => { if (!busyRef.current && !importGuard.current) void refresh() }, 6000)
    return () => window.clearInterval(timer)
  }, [refresh])
  useEffect(() => {
    const unsub = [bridge.on('transfer:progress', (p: Progress) => {
      const next = progressFromEvent(p)
      setProgress(next)
      const id = transferActivityId.current
      if (id) {
        const terminal = next.stage === 'error' || next.stage === 'cancelled' || (next.stage === 'done' && next.total > 0 && next.current >= next.total)
        const status: ActivityEntry['status'] = !terminal ? 'running' : next.stage === 'done' ? 'success' : next.stage === 'cancelled' ? 'cancelled' : 'error'
        updateActivity(id, { status, current: next.stage === 'done' ? next.current : Math.max(0, next.current - 1), total: next.total, detail: [next.title, next.message || stageName(next.stage)].filter(Boolean).join(' · ') })
        if (terminal) {
          transferGuard.current = false
          if (next.stage === 'done' && next.total > 0 && next.current >= next.total) notify({ text: `${next.title || 'Books'} sent to device`, kind: 'success' })
          else if (next.stage === 'error' || next.stage === 'cancelled') updateActivity(id, { detail: next.message || stageName(next.stage) })
        }
      } else if (next.stage === 'error' || next.stage === 'cancelled' || (next.stage === 'done' && next.total > 0 && next.current >= next.total)) transferGuard.current = false
    }), bridge.on('import:progress', (p: ImportProgress) => {
      if (importActivityId.current) updateActivity(importActivityId.current, { status: p.stage === 'error' ? 'error' : 'running', current: p.current, total: p.total, detail: p.message || p.filename })
    }), bridge.on('library:changed', () => void refresh()), bridge.on('devices:changed', () => void refresh())]
    const nativeDrop = window.runtime?.OnFileDrop
    const targetAt = (x: number, y: number): 'device' | 'library' => document.elementFromPoint(x, y)?.closest<HTMLElement>('[data-drop-target]')?.dataset.dropTarget === 'device' ? 'device' : 'library'
    const drop = (e: DragEvent) => { e.preventDefault(); setDragging(false); const files = Array.from(e.dataTransfer?.files ?? []); const paths = files.map((f: any) => f.path || (bridge.preview ? `/preview/${f.name}` : '')).filter(Boolean); if (paths.length) importPathsRef.current(paths, targetAt(e.clientX, e.clientY)) }
    const over = (e: DragEvent) => { e.preventDefault(); setDragging(true) }
    const leave = (e: DragEvent) => { if (!e.relatedTarget) setDragging(false) }
    if (nativeDrop) nativeDrop((x, y, paths) => { setDragging(false); if (paths.length) importPathsRef.current(paths, targetAt(x, y)) }, true)
    else { window.addEventListener('drop', drop); window.addEventListener('dragover', over); window.addEventListener('dragleave', leave) }
    return () => { unsub.forEach(fn => typeof fn === 'function' && fn()); if (nativeDrop) window.runtime?.OnFileDropOff?.(); else { window.removeEventListener('drop', drop); window.removeEventListener('dragover', over); window.removeEventListener('dragleave', leave) } }
  }, [refresh])
  useEffect(() => { if (progress?.stage === 'done' && progress.total > 0 && progress.current >= progress.total) { const timer = window.setTimeout(() => setProgress(null), 5000); return () => clearTimeout(timer) } }, [progress?.stage, progress?.current, progress?.total])
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
    const now = Date.now()
    const filtered = all.filter(b => {
      const timestamp = page === 'recent' ? Date.parse(b.addedAt) : 0
      return (!needle || `${b.title} ${b.authors.join(' ')}`.toLowerCase().includes(needle)) && matchesFilter(b.authors, authorFilter) && matchesFilter(b.subjects, genreFilter) && matchesFilter(b.language ? [b.language] : [], languageFilter) && (page !== 'recent' || (Number.isFinite(timestamp) && timestamp >= now - recentWindowMs && timestamp <= now))
    })
    return filtered.sort((a, b) => page === 'recent' ? Date.parse(b.addedAt) - Date.parse(a.addedAt) : sort === 'title' ? a.title.localeCompare(b.title) : sort === 'author' ? (a.authors[0] ?? '').localeCompare(b.authors[0] ?? '') : b.addedAt.localeCompare(a.addedAt))
  }, [state, query, page, sort, authorFilter, genreFilter, languageFilter])
  const recentGroups = useMemo(() => {
    if (page !== 'recent') return []
    const today = new Date()
    const yesterday = new Date(today)
    yesterday.setDate(yesterday.getDate() - 1)
    const batches = new Map<string, { id: string; timestamp: number; totalBooks: number; books: Book[] }>()
    const visibleIDs = new Set(books.map(book => book.id))
    for (const book of state?.books ?? []) {
      const timestamp = Date.parse(book.addedAt)
      if (!Number.isFinite(timestamp) || timestamp > today.getTime()) continue
      const id = book.importBatchID ? `batch:${book.importBatchID}` : `book:${book.id}`
      const batch = batches.get(id) ?? { id, timestamp, totalBooks: 0, books: [] }
      batch.timestamp = Math.max(batch.timestamp, timestamp)
      batch.totalBooks += 1
      if (visibleIDs.has(book.id)) batch.books.push(book)
      batches.set(id, batch)
    }
    const groups = new Map<string, { label: string; timestamp: number; batches: { id: string; timestamp: number; totalBooks: number; books: Book[] }[] }>()
    for (const batch of batches.values()) {
      if (!batch.books.length) continue
      const date = new Date(batch.timestamp)
      const key = localDateKey(date)
      const group = groups.get(key) ?? { label: recentDayLabel(date, today, yesterday), timestamp: batch.timestamp, batches: [] }
      group.timestamp = Math.max(group.timestamp, batch.timestamp)
      group.batches.push(batch)
      groups.set(key, group)
    }
    return [...groups.values()].map(group => ({ ...group, batches: group.batches.sort((a, b) => b.timestamp - a.timestamp) })).sort((a, b) => b.timestamp - a.timestamp)
  }, [state, books, page])
  const availableDevices = (state?.devices ?? []).filter(item => !hiddenEjectingDevices.current.has(item.id) && !ejectingDeviceIDs.current.has(item.id))
  const device = availableDevices.find(item => item.id === activeDeviceID) ?? availableDevices[0]
  useEffect(() => {
    if (device?.id) localStorage.setItem('attune-active-device', device.id)
  }, [device?.id])
  const isDevice = page === 'device'
  const visibleDeviceBooks = useMemo(() => deviceBooks.filter(book => {
    const needle = query.trim().toLowerCase()
    const title = book.title || fileTitle(book.name)
    return (!needle || `${title} ${book.name} ${(book.authors ?? []).join(' ')}`.toLowerCase().includes(needle)) && matchesFilter(book.authors, authorFilter) && matchesFilter(book.subjects, genreFilter) && matchesFilter(book.language ? [book.language] : [], languageFilter)
  }).sort((a, b) => sort === 'title' ? (a.title || fileTitle(a.name)).localeCompare(b.title || fileTitle(b.name)) : sort === 'author' ? (a.authors?.[0] ?? '').localeCompare(b.authors?.[0] ?? '') : 0), [deviceBooks, query, authorFilter, genreFilter, languageFilter, sort])
  const transferActive = transferGuard.current || (!!progress && !['error', 'cancelled'].includes(progress.stage) && !(progress.stage === 'done' && (!progress.total || progress.current >= progress.total)))
  const canSendToDevice = !!device && !device.requiresExplicitSelection && !transferActive
  const deviceSendLabel = !device ? 'Connect a reader to send' : device.requiresExplicitSelection ? 'Set transfer settings first' : transferActive ? 'Transfer in progress' : `Send to ${device.name}`
  useEffect(() => {
    const keys = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement | null
      const typing = !!target && (target.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName))
      if (confirmation && e.key !== 'Escape') return
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'v' && !typing) { e.preventDefault(); pasteFilesRef.current(); return }
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'f') { e.preventDefault(); (readerOpen ? readerSearchRef.current : searchRef.current)?.focus() }
      if (readerOpen && !typing && e.key === 'ArrowLeft') { e.preventDefault(); readerTurnRef.current(-1) }
      if (readerOpen && !typing && e.key === 'ArrowRight') { e.preventDefault(); readerTurnRef.current(1) }
      if (e.key === 'Escape') {
        if (readerOpen && document.fullscreenElement) { e.preventDefault(); void document.exitFullscreen(); return }
        if (activeMenu || pasteMenu) { setActiveMenu(null); setPasteMenu(null); e.preventDefault(); return }
        if (intake) setIntake(null)
        else if (confirmation) setConfirmation(null)
        else if (readerOpen) { setReaderOpen(false); setPreviewTarget(null); setBookPreview(null) }
        else { setFocusedBook(null); setFocusedDeviceBook(null); setEditing(false); setSelected([]) }
      }
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'a' && !typing) {
        e.preventDefault()
        if (page === 'device') setDeviceSelection(visibleDeviceBooks.map(b => b.path))
        else if (page === 'library' || page === 'recent') setSelected(books.map(b => b.id))
      }
    }
    const paste = (event: ClipboardEvent) => {
      const target = event.target as HTMLElement | null
      const typing = !!target && (target.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName))
      if (typing) return
      event.preventDefault()
      pasteFilesRef.current()
    }
    window.addEventListener('keydown', keys)
    window.addEventListener('paste', paste)
    return () => { window.removeEventListener('keydown', keys); window.removeEventListener('paste', paste) }
  }, [page, visibleDeviceBooks, books, confirmation, previewTarget, readerOpen, bookPreview, activeMenu])

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
    catch (e) { notify({ text: message(e), kind: 'error' }); return null }
    finally { setBusy(false) }
  }
  async function performImport(action: () => Promise<ImportResult>, total = 0, filename = ''): Promise<ImportResult | null> {
    if (importGuard.current || busy || transferActive || changingLibrary) return null
    importGuard.current = true
    setBusy(true)
    const activityID = begin('import', total > 1 ? `Import ${total} books` : filename ? `Import ${filename}` : 'Import books')
    importActivityId.current = activityID
    try {
      const result = await action()
      if (!result || (!result.imported && !result.duplicates && !result.skipped && !result.errors?.length)) {
        removeActivity(activityID)
        return result ?? null
      }
      const errors = result.errors ?? []
      const imported = result.imported ?? 0
      const duplicates = result.duplicates ?? 0
      const skipped = result.skipped ?? 0
      const processed = imported + duplicates + skipped + errors.length
      const summary = `${imported} imported · ${duplicates} duplicates${skipped ? ` · ${skipped} skipped` : ''}`
      updateActivity(activityID, { status: errors.length ? 'error' : 'success', current: processed, total: total || processed, detail: errors.length ? `${summary} · ${errors.length} file issues` : summary })
      if (errors.length) {
        setImportReport(result)
        notify({ text: `${summary} · ${errors.length} file issues. Review the import report for details.`, kind: 'error' })
      }
      else notify({ text: summary, kind: 'success' })
      await refresh(true)
      return result
    } catch (e) {
      updateActivity(activityID, { status: 'error', detail: message(e) })
      notify({ text: message(e), kind: 'error' })
      return null
    } finally {
      importActivityId.current = null
      importGuard.current = false
      setBusy(false)
    }
  }
  async function importPaths(paths: string[], target: 'library' | 'device' = 'library') {
    if (!paths.length || intake || intakeFlowGuard.current || clipboardReadInFlight.current || importGuard.current || busy || transferActive || changingLibrary) return
    const names = paths.map(path => path.split(/[\\/]/).pop() ?? path)
    if (target === 'device') setIntake({ paths, names, device: device && !device.requiresExplicitSelection ? { id: device.id, name: device.name } : null })
    else await completeIntake({ paths, names, device: null }, false)
  }
  importPathsRef.current = (paths, target) => { void importPaths(paths, target) }
  async function requestPasteFiles() {
    if (Date.now() - pasteHandledAt.current < 350 || clipboardReadInFlight.current || intake || intakeFlowGuard.current) return
    pasteHandledAt.current = Date.now()
    setPasteMenu(null)
    if (intakeFlowGuard.current || importGuard.current || busy || transferActive || changingLibrary) { notify({ text: 'Finish the current operation before importing more books.', kind: 'status' }); return }
    clipboardReadInFlight.current = true
    try {
      const paths = await bridge.readClipboardFiles()
      if (!paths.length) { notify({ text: 'No supported book files were found on the clipboard.', kind: 'error' }); return }
      const names = paths.map(path => path.split(/[\\/]/).pop() ?? path)
      setIntake({ paths, names, device: device && !device.requiresExplicitSelection ? { id: device.id, name: device.name } : null })
    } catch (e) { notify({ text: message(e), kind: 'error' }) }
    finally { clipboardReadInFlight.current = false }
  }
  pasteFilesRef.current = () => { void requestPasteFiles() }
  async function completeIntake(request: IntakeRequest, sendToDevice: boolean) {
    if (importGuard.current || busy || transferActive || changingLibrary) return
    const result = await performImport(() => bridge.importPaths(request.paths), request.paths.length, request.names[0] ?? 'Imported books')
    if (!sendToDevice || !result || !result.bookIDs?.length) return
    const target = request.device
    if (!target) { notify({ text: 'No reader was connected when you chose to send these books.', kind: 'error' }); return }
    try {
      const fresh = await bridge.getState()
      if (!fresh.devices.some(candidate => candidate.id === target.id)) { notify({ text: `${target.name} disconnected. Your books were imported to the library but were not sent.`, kind: 'error' }); return }
      intakeFlowGuard.current = false
      await send(result.bookIDs, target)
    } catch (e) { notify({ text: message(e), kind: 'error' }) }
  }
  async function confirmIntake(choice: 'send' | 'library') {
    if (!intake) return
    const request = intake
    setIntake(null)
    intakeFlowGuard.current = true
    try { await completeIntake(request, choice === 'send') }
    finally { intakeFlowGuard.current = false }
  }
  async function beginImport() {
    if (intakeFlowGuard.current || importGuard.current || busy || transferActive || changingLibrary) return
    if (bridge.preview) await performImport(() => bridge.previewImport(), 18, 'Preparing sample EPUBs…')
    else await performImport(() => bridge.importBooks())
  }
  async function importCalibreLibrary(): Promise<ImportResult | null> {
    if (intakeFlowGuard.current || busy || transferActive || changingLibrary) return null
    return await performImport(() => bridge.importCalibreLibrary(), 0, 'Calibre library')
  }
  async function completeOnboarding() {
    await bridge.completeOnboarding()
    onboardingReady.current = true
    await refresh(true)
    setOnboarding(current => current ? { ...current, required: false } : current)
    setOnboardingError('')
  }
  async function importCalibreAt(path: string) {
    return await performImport(() => bridge.importCalibreLibraryAt(path), 0, 'Calibre library')
  }
  async function addDevice() {
    if (ejectingDeviceIDs.current.size) return
    await run(() => bridge.selectDevice(), async () => { await refresh(); navigateTo('device') })
  }
  async function openDevicePreferences(target: Device) {
    const preferences = await run(() => bridge.getDevicePreferences(target.id))
    if (preferences) setDevicePreferencesDraft({ ...preferences, deviceID: target.id, deviceName: target.name, foldersText: preferences.folders.join('\n'), requiresExplicitSelection: !!target.requiresExplicitSelection })
  }
  async function saveDevicePreferences() {
    if (!devicePreferencesDraft) return
    const folders = devicePreferencesDraft.foldersText.split('\n').map(folder => folder.trim()).filter(Boolean)
    await run(() => bridge.saveDevicePreferences(devicePreferencesDraft.deviceID, folders, devicePreferencesDraft.formats), async () => {
      setDevicePreferencesDraft(null)
      await refresh(true)
      notify({ text: `Transfer settings saved for ${devicePreferencesDraft.deviceName}`, kind: 'success' })
    })
  }
  async function resetDevicePreferences() {
    if (!devicePreferencesDraft) return
    await run(() => bridge.resetDevicePreferences(devicePreferencesDraft.deviceID), async () => {
      const name = devicePreferencesDraft.deviceName
      setDevicePreferencesDraft(null)
      await refresh(true)
      notify({ text: `Transfer defaults restored for ${name}`, kind: 'success' })
    })
  }
  async function changeLibraryLocation() {
    if (libraryChangeGuard.current || intakeFlowGuard.current || busy || importGuard.current || transferActive) return
    libraryChangeGuard.current = true
    setChangingLibrary(true)
    try {
      await run(() => bridge.changeLibraryLocation(), async (newRoot: string) => {
        if (!newRoot) return
        setFocusedBook(null)
        setFocusedDeviceBook(null)
        setSelected([])
        setDeviceSelection([])
        setEditing(false)
        setActiveMenu(null)
        setPreviewTarget(null)
        setBookPreview(null)
        await refresh(true)
        notify({ text: 'Library location updated. The original copy is still in its previous folder.', kind: 'success' })
      })
    } finally {
      libraryChangeGuard.current = false
      setChangingLibrary(false)
    }
  }
  async function startEdit(book: Book) {
    setFocusedDeviceBook(null)
    setFocusedBook(book)
    setEditDraft({ title: book.title, authors: book.authors.join('; '), publisher: book.publisher ?? '', language: book.language ?? '', published: book.published ?? '', subjects: (book.subjects ?? []).join('; '), series: book.series ?? '', seriesIndex: book.seriesIndex ?? '', description: book.description ?? '' })
    setEditing(true)
  }
  async function changeCover(book: Book, file?: File) {
    if (busy || importGuard.current || transferActive || changingLibrary || intakeFlowGuard.current) return
    if (bridge.preview && !file) {
      coverTarget.current = book
      coverInputRef.current?.click()
      return
    }
    await run(() => bridge.changeBookCover(book.id, file), async (updated: Book) => {
      if (!updated?.id) return
      setFocusedBook(current => current?.id === updated.id ? updated : current)
      await refresh(true)
      notify({ text: 'Cover updated', kind: 'success' })
    })
  }
  async function attachBookFiles(book: Book) {
    const result = await performImport(() => bridge.attachBookFiles(book.id), 0, `${book.title} files`)
    if (!result) return
    const next = await bridge.getState()
    setState(next)
    setFocusedBook(next.books.find(item => item.id === book.id) ?? book)
  }
  async function openPreview(book: Book, requestedFileID?: string) {
    setPreviewTarget(book)
    setReaderOpen(true)
    setBookPreview(null)
    setReaderSearch('')
    setPreviewError('')
    setPreviewLoading(true)
    try {
      if (bridge.preview) {
        const next = await bridge.getBookPreview(book.id)
        setBookPreview(next)
        const saved = await bridge.getReadingProgress(book.id).catch(() => ({ bookID: book.id, chapterIndex: 0, position: 0 }))
        setPreviewChapter(Math.max(0, Math.min(next.chapters.length - 1, saved.chapterIndex)))
      } else {
        const readable = (book.files ?? []).filter(file => file.canRead)
        const chosen = readable.find(file => file.id === requestedFileID) ?? [...readable].sort((left, right) => {
          const leftOrder = readerFormatOrder.indexOf(left.format.toLowerCase())
          const rightOrder = readerFormatOrder.indexOf(right.format.toLowerCase())
          return (leftOrder < 0 ? Number.MAX_SAFE_INTEGER : leftOrder) - (rightOrder < 0 ? Number.MAX_SAFE_INTEGER : rightOrder) || Number(right.preferred) - Number(left.preferred)
        })[0]
        if (!chosen) throw new Error('This book has no format that Attune can read yet. Add a supported format or open a file with another app.')
        setBookPreview({ book, chapters: [], fileID: chosen.id })
        const legacy = await bridge.getReadingProgress(book.id).catch(() => ({ bookID: book.id, chapterIndex: 0, position: 0 }))
        setPreviewChapter(legacy.chapterIndex)
      }
    }
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
    setConfirmation({ title: 'Remove from library?', description: `“${book.title}” will be removed from your library. Copies already on a reader will stay there.`, confirmLabel: 'Remove from library', action: () => { void run(() => bridge.deleteBook(book.id), async () => { setSelected(s => s.filter(id => id !== book.id)); setFocusedBook(null); await refresh(); notify({ text: 'Book removed from your library', kind: 'success' }) }) } })
  }
  async function deleteSelected() {
    const targets = (state?.books ?? []).filter(b => selected.includes(b.id))
    if (!targets.length) return
    setConfirmation({ title: `Remove ${targets.length} ${targets.length === 1 ? 'book' : 'books'} from library?`, description: 'Copies already on a reader will stay there.', confirmLabel: 'Remove from library', action: () => { void run(async () => { for (const book of targets) await bridge.deleteBook(book.id) }, async () => { setSelected([]); await refresh(); notify({ text: `${targets.length} ${targets.length === 1 ? 'book' : 'books'} removed from your library`, kind: 'success' }) }) } })
  }
  async function send(ids: string[], target: { id: string; name: string } | null = device ? { id: device.id, name: device.name } : null) {
    if (!ids.length) return
    if (intakeFlowGuard.current || transferActive) { notify({ text: 'Wait for the current operation to finish before starting another.', kind: 'status' }); return }
    if (!target) { notify({ text: 'Connect a reader first, then choose it in Devices.', kind: 'error' }); return }
    transferGuard.current = true
    const title = ids.length === 1 ? (state?.books ?? []).find(b => b.id === ids[0])?.title ?? 'Book' : `${ids.length} books`
    transferActivityId.current = begin('transfer', `Send ${title} to ${target.name}`)
    updateActivity(transferActivityId.current, { status: 'running', current: 0, total: ids.length, detail: 'Preparing transfer' })
    setProgress({ id: 'starting', bookID: '', title, stage: 'queued', message: 'Preparing transfer', current: 0, total: ids.length })
    const ok = await run(() => bridge.sendBooks(target.id, ids), () => setSelected([]))
    if (ok === null) {
      transferGuard.current = false
      if (transferActivityId.current) updateActivity(transferActivityId.current, { status: 'error', detail: 'Could not start transfer. Check the device connection and try again.' })
      setProgress(p => p ? { ...p, stage: 'error', message: 'Could not start transfer. Check the device connection and try again.' } : null)
    }
  }
  function eject() {
    if (!device) return
    const target = device
    setConfirmation({ title: `Eject ${target.name}?`, description: 'Finish writing to the device and disconnect it safely.', confirmLabel: 'Eject device', action: () => {
      if (ejectingDeviceIDs.current.has(target.id)) return
      hiddenEjectingDevices.current.add(target.id)
      ejectingDeviceIDs.current.add(target.id)
      setEjectingDevice(target)
      dismiss()
      setFocusedDeviceBook(null)
      setActiveMenu(null)
      setState(current => current ? { ...current, devices: current.devices.filter(item => item.id !== target.id) } : current)
      const activityID = begin('eject', `Eject ${target.name}`)
      updateActivity(activityID, { detail: 'Waiting for the device to finish disconnecting safely' })
      void (async () => {
        setBusy(true)
        try {
          await bridge.ejectDevice(target.id)
          updateActivity(activityID, { status: 'success', detail: 'Device disconnected safely' })
          notify({ text: `${target.name} ejected safely`, kind: 'success' })
          await refresh()
        } catch (e) {
          hiddenEjectingDevices.current.delete(target.id)
          ejectingDeviceIDs.current.delete(target.id)
          updateActivity(activityID, { status: 'error', detail: message(e) })
          setState(current => current && !current.devices.some(item => item.id === target.id) ? { ...current, devices: [...current.devices, target] } : current)
          notify({ text: message(e), kind: 'error' })
          await refresh(true)
        } finally {
          ejectingDeviceIDs.current.delete(target.id)
          setEjectingDevice(null)
          setBusy(false)
        }
      })()
    } })
  }
  function confirmRemoveFromDevice(paths: string[]) {
    if (!device || !paths.length) return
    const single = paths.length === 1
    setConfirmation({ title: `Remove ${paths.length} ${single ? 'book' : 'books'} from ${device.name}?`, description: 'Your library copies will stay on this computer.', confirmLabel: 'Remove from device', action: () => { void run(() => bridge.removeDeviceBooks(device.id, paths), async () => { setDeviceSelection(s => s.filter(path => !paths.includes(path))); setDeviceBooks(await bridge.getDeviceBooks(device.id)); notify({ text: `${single ? 'Book' : 'Books'} removed from ${device.name}`, kind: 'success' }) }) } })
  }
  function removeFromDevice() { confirmRemoveFromDevice([...deviceSelection]) }
  function toggle(id: string, e?: React.MouseEvent) { setSelected(s => s.includes(id) ? s.filter(x => x !== id) : e?.shiftKey ? [...s, id] : [...s, id]) }

  const hasFilters = !!(authorFilter || genreFilter || languageFilter)
  const openDeviceDetails = (book: DeviceBook) => { setFocusedBook(null); setFocusedDeviceBook(book) }
  const renderBook = (book: Book, itemLayout: 'grid' | 'list') => (
    <BookTile key={book.id} book={book} layout={itemLayout} selected={selected.includes(book.id)} menu={activeMenu?.key === `book:${book.id}` ? activeMenu : null} onMenu={button => toggleMenu(`book:${book.id}`, button)} onMenuClose={() => setActiveMenu(null)} canRead={!!book.files?.some(file => file.canRead)} canSend={canSendToDevice} sendLabel={deviceSendLabel} onPreview={() => { setActiveMenu(null); void openPreview(book) }} onEdit={() => { setActiveMenu(null); void startEdit(book) }} onSend={() => { setActiveMenu(null); void send([book.id]) }} onDelete={() => { setActiveMenu(null); void deleteLocal(book) }} onToggle={e => toggle(book.id, e)} onOpen={() => { setFocusedDeviceBook(null); setFocusedBook(book); setEditing(false) }}/>
  )
  function toggleMenu(key: string, button: HTMLButtonElement) {
    if (activeMenu?.key === key) { setActiveMenu(null); return }
    const rect = button.getBoundingClientRect()
    const width = 220
    setActiveMenu({ key, left: Math.max(8, Math.min(window.innerWidth - width - 8, rect.right - width)), top: Math.max(8, Math.min(window.innerHeight - 190, rect.bottom + 6)) })
  }
  const restartDisabled = busy || importGuard.current || transferActive || changingLibrary
  async function downloadUpdate() {
    const next = await updater.download()
    if (next?.downloaded) setConfirmation(current => current ?? { title: 'Your update is ready', description: 'The download is verified. Restart now to use the new version, or keep using Attune and it will be active the next time you open the app.', confirmLabel: 'Restart now', cancelLabel: 'Later', danger: false, action: () => { void updater.restart() } })
  }
  const detailsVisible = !readerOpen && page !== 'settings' && page !== 'updates'
  const detailsOpen = detailsVisible && !!(focusedBook || focusedDeviceBook)
  const resizeHandle = <ResizeHandle width={detailWidth} onWidthChange={setDetailWidth}/>
  if (!onboarding || onboarding.required || onboardingError) {
    if (!onboarding && !onboardingError) return <main className={`welcome-shell ${bridge.preview ? 'preview-mode' : ''}`}><section className="welcome-panel" role="status"><img className="welcome-app-icon" src="/attune.svg" alt=""/><h1 id="welcome-title">Attune your library.</h1><p>Checking your library setup…</p></section></main>
    return <Welcome libraries={onboarding?.libraries ?? []} error={onboardingError || undefined} onRetry={() => void loadOnboarding()} onScan={() => bridge.findCalibreLibraries(true)} onComplete={completeOnboarding} onImport={importCalibreAt} onBrowse={importCalibreLibrary} preview={bridge.preview}/>
  }
  return <div className={`app-shell ${bridge.preview ? 'preview-mode' : ''} ${detailsOpen ? 'details-open' : ''}`} style={detailsOpen ? { gridTemplateColumns: `64px minmax(320px, 1fr) ${detailWidth}px` } : undefined}>
    <aside className="sidebar" aria-label="Main navigation">
      <nav className="rail-nav" aria-label="Library sections">
        <button className={`rail-item ${page === 'library' ? 'active' : ''}`} aria-label="All books" aria-current={page === 'library' ? 'page' : undefined} onClick={() => navigateTo('library')}><LibraryBig/></button>
        <button className={`rail-item ${page === 'recent' ? 'active' : ''}`} aria-label="Recently added" aria-current={page === 'recent' ? 'page' : undefined} onClick={() => navigateTo('recent')}><Clock3/></button>
        <button className={`rail-item ${page === 'device' ? 'active' : ''}`} data-drop-target={device ? 'device' : 'library'} aria-label="Reader" aria-current={page === 'device' ? 'page' : undefined} onClick={() => navigateTo('device')}>{device ? <span className="device-glyph"><HardDrive/><i/></span> : <HardDrive/>}</button>
        {previewTarget && <button className={`rail-item ${readerOpen ? 'active' : ''}`} aria-label="Return to book" title="Return to book" aria-current={readerOpen ? 'page' : undefined} onClick={() => setReaderOpen(true)}><BookOpen/></button>}
      </nav>
      <div className="rail-spacer"/>
      {(updater.state.available || updater.state.downloaded) && <button className={`rail-item rail-updates ${page === 'updates' ? 'active' : ''}`} aria-label="Update available" aria-busy={updater.state.status === 'downloading'} aria-current={page === 'updates' ? 'page' : undefined} onClick={() => { navigateTo('updates'); if (!updater.state.downloaded && updater.state.status !== 'downloading') void downloadUpdate() }}><Download/></button>}
      <button className={`rail-item rail-settings ${page === 'settings' ? 'active' : ''}`} aria-label="Settings" aria-current={page === 'settings' ? 'page' : undefined} onClick={() => navigateTo('settings')}><Settings2/></button>
    </aside>

    <header className="topbar" data-drop-target={isDevice ? 'device' : 'library'}>
      <div className="topbar-leading">
        {!readerOpen && <div className="topbar-navigation" aria-label="Navigation history">
          <button className="history-button" aria-label="Back" title="Back" disabled={navigation.index === 0} onClick={() => setNavigation(current => ({ ...current, index: Math.max(0, current.index - 1) }))}><ArrowLeft size={17}/></button>
          <button className="history-button" aria-label="Forward" title="Forward" disabled={navigation.index >= navigation.pages.length - 1} onClick={() => setNavigation(current => ({ ...current, index: Math.min(current.pages.length - 1, current.index + 1) }))}><ArrowRight size={17}/></button>
        </div>}
        {readerOpen && previewTarget ? <><button className="history-button" aria-label="Previous page" title="Previous page" onClick={() => readerTurnRef.current(-1)}><ArrowLeft size={17}/></button><button className="history-button" aria-label="Next page" title="Next page" onClick={() => readerTurnRef.current(1)}><ArrowRight size={17}/></button><h1 className="global-page-title reader-global-title">{previewTarget.title}</h1></> : <h1 className="global-page-title">{page === 'library' ? `All books (${books.length})` : page === 'recent' ? 'Recently added' : page === 'device' ? 'Reader' : page === 'settings' ? 'Settings' : 'Updates'}</h1>}
      </div>
      <div className="topbar-center">
        {readerOpen && previewTarget ? <label className="search-box"><Search size={17}/><input ref={readerSearchRef} placeholder="Find in book" aria-label="Find in book" value={readerSearch} onChange={e => setReaderSearch(e.target.value)} onKeyDown={e => { if (e.key === 'Enter') readerFindRef.current() }}/><kbd>↵</kbd></label> : <>{page !== 'settings' && page !== 'updates' && <ActivityCenter entries={entries} onCancelTransfer={() => void run(() => bridge.cancelTransfer())}/ >}{page !== 'settings' && page !== 'updates' && <label className="search-box"><Search size={17}/><input ref={searchRef} placeholder="Search books" aria-label="Search books" value={query} onChange={e => setQuery(e.target.value)}/><kbd>{/Mac|iPhone|iPad/.test(navigator.platform) ? '⌘ F' : 'Ctrl+F'}</kbd>{query && <button className="clear-search" onClick={() => setQuery('')} aria-label="Clear search"><X size={14}/></button>}</label>}</>}
      </div>
      <div className="top-actions">
        {readerOpen && previewTarget ? <button className="toolbar-button" title="Close reader" onClick={() => { setReaderOpen(false); setPreviewTarget(null); setBookPreview(null) }}><X size={16}/><span>Close</span></button> : <>{page === 'device' && device && <button className="toolbar-button" title="Refresh reader contents" onClick={() => deviceRefreshRef.current()}><RefreshCw size={16}/><span>Refresh</span></button>}{page !== 'settings' && page !== 'updates' && <button className="toolbar-button primary-button" onClick={() => void beginImport()} disabled={busy || transferActive}><Plus size={17}/><span>Import books</span></button>}</>}
      </div>
    </header>

    <main className="main-panel" style={{ '--book-scale': bookSize / 100 } as React.CSSProperties} data-drop-target={isDevice ? 'device' : 'library'} onContextMenu={e => { if (readerOpen || (page !== 'library' && page !== 'recent' && page !== 'device')) return; const target = e.target as HTMLElement; if (target.closest('button,input,textarea,select,a,[contenteditable=true],.book-tile,.book-row,.device-book,.book-context-menu,.details-panel,.filter-popover,.activity-center')) return; e.preventDefault(); setActiveMenu(null); setPasteMenu({ left: Math.min(e.clientX, window.innerWidth - 220), top: Math.min(e.clientY, window.innerHeight - 56) }) }}>
      {readerOpen && previewTarget ? <ReaderView book={bookPreview?.book ?? previewTarget} preview={bookPreview} loading={previewLoading} error={previewError} chapterIndex={previewChapter} setChapterIndex={setPreviewChapter} search={readerSearch} findRef={readerFindRef} turnRef={readerTurnRef}/> : page === 'settings' ? <Settings updates={updater.state} onUpdates={() => { navigateTo('updates'); void updater.check() }} textSize={textSize} setTextSize={setTextSize} state={state} onOpenFolder={() => void run(() => bridge.openLibraryFolder())} onChangeLocation={() => void changeLibraryLocation()} onImportCalibre={() => void importCalibreLibrary()} changingLibrary={changingLibrary} controlsDisabled={busy || importGuard.current || transferActive}/> : page === 'updates' ? <Updates state={updater.state} checked={updater.checked} onCheck={() => void updater.check()} onDownload={() => void downloadUpdate()} onRestart={() => void updater.restart()} restartDisabled={restartDisabled}/> : <>
        {isDevice && device ? <section className="device-overview">
          <div className="device-hero-icon"><HardDrive size={25}/></div><div className="device-copy"><div className="eyebrow">CONNECTED DEVICE</div><h1>{device.name}</h1><div className="device-path">{device.transport === 'usb-storage' ? device.root : 'Connected over MTP'}</div>{device.requiresExplicitSelection && <div className="device-profile-warning">Set a book folder and supported formats before sending.</div>}</div>
          {availableDevices.length > 1 && <div className="device-select-control"><DropdownSelect label="Connected reader" value={device.id} onChange={setActiveDeviceID} options={availableDevices.map(item => ({ value: item.id, label: item.name }))}/></div>}
          <div className="storage"><div className="storage-line"><span>{fmtBytes(device.freeBytes)} available</span><span>of {fmtBytes(device.totalBytes)}</span></div><div className="storage-track"><span style={{ width: `${Math.max(2, 100 - device.freeBytes / Math.max(device.totalBytes, 1) * 100)}%` }}/></div></div>
          <button className="toolbar-button" title={`Transfer settings for ${device.name}`} onClick={() => void openDevicePreferences(device)} disabled={busy || transferActive}><Settings2 size={16}/><span>Transfer settings</span></button>
          <button className="toolbar-button eject-button" title={`Eject ${device.name}`} aria-label={`Eject ${device.name}`} onClick={eject}><Power size={16}/><span>Eject</span></button>
        </section> : null}
        <section className="content-heading">
          <div className="library-toolbar">
            {isDevice && <span className="book-count">{`${visibleDeviceBooks.length} ${visibleDeviceBooks.length === 1 ? 'book' : 'books'}`}</span>}
            <FilterPicker authorFilter={authorFilter} genreFilter={genreFilter} languageFilter={languageFilter} metadata={metadata} setAuthorFilter={setAuthorFilter} setGenreFilter={setGenreFilter} setLanguageFilter={setLanguageFilter}/>
            {page !== 'recent' &&
              <div className="sort-select" title="Sort books"><ArrowDownUp size={15}/><DropdownSelect label="Sort books" value={sort} onChange={value => setSort(value as Sort)} options={[{ value: 'recent', label: isDevice ? 'Device order' : 'Recently added' }, { value: 'title', label: 'Title' }, { value: 'author', label: 'Author' }]}/></div>
            }
            <div className="view-controls">
              <BookSizeControl value={bookSize} onChange={setBookSize}/>
              {page !== 'recent' && <div className="segmented" role="group" aria-label="View layout"><button aria-label="Grid view" aria-pressed={layout === 'grid'} className={layout === 'grid' ? 'chosen' : ''} onClick={() => setLayout('grid')}><Grid2X2 size={16}/></button><button aria-label="List view" aria-pressed={layout === 'list'} className={layout === 'list' ? 'chosen' : ''} onClick={() => setLayout('list')}><List size={17}/></button></div>}
            </div>
            {isDevice ? <>{deviceSelection.length > 0 && <button className="toolbar-button danger-button compact-action" title={`Remove ${deviceSelection.length} selected books from device`} aria-label={`Remove ${deviceSelection.length} selected books from device`} onClick={() => void removeFromDevice()}><Trash2 size={16}/><span>Remove ({deviceSelection.length})</span></button>}{device && <button className="toolbar-button compact-action" title="Add from library" aria-label="Add from library" onClick={() => navigateTo('library')}><Send size={16}/><span>Add from library</span></button>}</> : <>
              {selected.length > 0 && <button className="toolbar-button danger-button compact-action" title={`Remove ${selected.length} selected books`} aria-label={`Remove ${selected.length} selected books`} onClick={() => void deleteSelected()}><Trash2 size={16}/><span>Remove ({selected.length})</span></button>}
              {selected.length > 0 && device && <button className="toolbar-button compact-action" title={`Send ${selected.length} selected books to ${device.name}`} aria-label={`Send ${selected.length} selected books to ${device.name}`} onClick={() => void send(selected)} disabled={!canSendToDevice}><Send size={16}/><span>Send to {device.name} <em>{selected.length}</em></span></button>}
            </>}
          </div>
          {(authorFilter || genreFilter || languageFilter) && <div className="applied-filter-tags" aria-label="Applied filters">{authorFilter && <FilterTag label="Author" value={authorFilter === unknownFilter ? 'Unknown' : authorFilter} onRemove={() => setAuthorFilter('')}/ >}{genreFilter && <FilterTag label="Genre" value={genreFilter === unknownFilter ? 'Unknown' : genreFilter} onRemove={() => setGenreFilter('')}/ >}{languageFilter && <FilterTag label="Language" value={languageFilter === unknownFilter ? 'Unknown' : languageLabel(languageFilter)} onRemove={() => setLanguageFilter('')}/ >}<button className="clear-filters" onClick={() => { setAuthorFilter(''); setGenreFilter(''); setLanguageFilter('') }}>Clear all</button></div>}
        </section>
        <div className={`book-area ${dragging ? 'dragging' : ''}`} data-drop-target={isDevice ? 'device' : 'library'}>
          {dragging && <div className="drop-overlay"><div><Upload size={27}/><b>Drop books to add them</b><span>Your originals stay untouched</span></div></div>}
          {isDevice ? device ? <DeviceList deviceBooks={visibleDeviceBooks} deviceName={device.name} layout={layout} hasBooks={deviceBooks.length > 0} searching={!!query.trim() || hasFilters} loading={deviceBooksLoading} error={deviceBooksError} selection={deviceSelection} setSelection={setDeviceSelection} onClearSearch={() => { setQuery(''); setAuthorFilter(''); setGenreFilter(''); setLanguageFilter('') }} onRefresh={() => deviceRefreshRef.current()} onOpen={openDeviceDetails} menu={activeMenu} onMenu={(key, button) => toggleMenu(key, button)} onMenuClose={() => setActiveMenu(null)} onPreview={previewDeviceBook} onRemove={path => { setActiveMenu(null); confirmRemoveFromDevice([path]) }}/> : <DeviceDisconnected ejecting={ejectingDevice?.name ?? ''} onConnect={() => void addDevice()}/> : books.length ? page === 'recent' ? <div className="recent-timeline">
            {recentGroups.map(group => <section className="recent-day" key={group.label}><h2 className="recent-day-heading">{group.label}</h2>
              {group.batches.map(batch => { const time = formatAddedTime(batch.timestamp); return <section className="recent-batch" key={batch.id}>
                <div className="recent-batch-heading"><time dateTime={time?.dateTime} title={time?.title}>{time?.time}</time><span>{batch.books.length < batch.totalBooks ? `${batch.books.length} of ${batch.totalBooks} books shown` : `${batch.totalBooks} ${batch.totalBooks === 1 ? 'book' : 'books'} added`}</span></div>
                <div className="book-collection list-layout recent-batch-books">
                  {batch.books.map(book => renderBook(book, 'list'))}
                </div>
              </section> })}
            </section>)}
            <p className="recent-history-note">Entries older than 30 days aren’t shown.</p>
          </div> : <div className={`book-collection ${layout === 'list' ? 'list-layout' : ''}`}>
            {books.map(book => renderBook(book, layout))}
          </div> : <EmptyState recent={page === 'recent'} searching={!!query || hasFilters} onImport={() => void beginImport()} onClear={() => { setQuery(''); setAuthorFilter(''); setGenreFilter(''); setLanguageFilter('') }}/ >}
        </div>
      </>}
    </main>
    {importReport && <ImportReportDialog result={importReport} onClose={() => setImportReport(null)}/>}
    {devicePreferencesDraft && <DevicePreferencesDialog draft={devicePreferencesDraft} onChange={setDevicePreferencesDraft} onClose={() => setDevicePreferencesDraft(null)} onSave={() => void saveDevicePreferences()} onReset={() => void resetDevicePreferences()} disabled={busy || transferActive}/>}
    {detailsVisible && focusedBook && <div className="details-column">{resizeHandle}<DetailsPanel book={focusedBook} editing={editing} draft={editDraft} setDraft={setEditDraft} canSend={canSendToDevice} onClose={() => { setFocusedBook(null); setEditing(false) }} onEdit={() => void startEdit(focusedBook)} onSave={() => void saveEdit()} onCancel={() => setEditing(false)} onDelete={() => void deleteLocal(focusedBook)} onSend={() => void send([focusedBook.id])} onPreview={fileID => void openPreview(focusedBook, fileID)} onOpenFile={fileID => void run(() => bridge.openBookFile(focusedBook.id, fileID))} onAttachFiles={() => void attachBookFiles(focusedBook)} onChangeCover={() => void changeCover(focusedBook)} coverDisabled={busy || transferActive || changingLibrary}/> </div>}
    {bridge.preview && <input hidden type="file" ref={coverInputRef} accept="image/jpeg,image/png,.jpg,.jpeg,.png" aria-label="Choose cover image" onChange={e => { const file = e.currentTarget.files?.[0]; const book = coverTarget.current; e.currentTarget.value = ''; coverTarget.current = null; if (file && book) void changeCover(book, file) }}/ >}
    {detailsVisible && focusedDeviceBook && <div className="details-column">{resizeHandle}<DeviceDetailsPanel book={focusedDeviceBook} deviceName={device?.name ?? 'Reader'} libraryBook={(state?.books ?? []).find(book => book.id === focusedDeviceBook.bookID)} onClose={() => setFocusedDeviceBook(null)} onPreview={book => void openPreview(book)}/></div>}
    {pasteMenu && <div className="book-context-menu paste-context-menu" style={{ left: pasteMenu.left, top: pasteMenu.top }} role="menu" onContextMenu={e => e.preventDefault()}><button role="menuitem" onClick={() => { setPasteMenu(null); pasteFilesRef.current() }}><span className="menu-item-icon"><Upload size={16}/></span>Paste books <kbd>{/Mac|iPhone|iPad/.test(navigator.platform) ? '⌘V' : 'Ctrl+V'}</kbd></button></div>}
    {intake && <IntakeDialog request={intake} onCancel={() => setIntake(null)} onImportOnly={() => void confirmIntake('library')} onSend={() => void confirmIntake('send')}/>}
    {confirmation && <ConfirmDialog confirmation={confirmation} onCancel={() => setConfirmation(null)} onConfirm={() => { const action = confirmation.action; setConfirmation(null); action() }}/>}
  </div>
}

function BookTile({ book, layout, selected, menu, onMenu, onMenuClose, canRead, canSend, sendLabel, onPreview, onEdit, onSend, onDelete, onToggle, onOpen }: { book: Book; layout: 'grid' | 'list'; selected: boolean; menu: MenuAnchor | null; onMenu: (button: HTMLButtonElement) => void; onMenuClose: () => void; canRead: boolean; canSend: boolean; sendLabel: string; onPreview: () => void; onEdit: () => void; onSend: () => void; onDelete: () => void; onToggle: (e: React.MouseEvent) => void; onOpen: () => void }) {
  const cover = bookCover(book)
  return <article className={`book-tile ${layout === 'list' ? 'book-row' : ''} ${selected ? 'book-selected' : ''}`}>
    <button className="book-main" onClick={onOpen} aria-label={`View ${book.title}`}>
      <div className="cover-wrap"><img src={cover} alt={`${book.title} cover`} draggable="false" onError={e => { e.currentTarget.src = missingCover }}/><span className="cover-gloss"/><span className="quick-check" aria-hidden="true"><Check size={13}/></span></div>
      <span className="book-meta"><strong title={book.title}>{book.title}</strong><span title={book.authors.join(', ')}>{book.authors.join(', ') || 'Unknown author'}</span></span>
    </button>
    <div className="book-menu-anchor" data-book-menu><button className="tile-more" title={`Actions for ${book.title}`} aria-label={`Actions for ${book.title}`} aria-haspopup="menu" aria-expanded={!!menu} onClick={e => { e.stopPropagation(); onMenu(e.currentTarget) }}><MoreHorizontal size={18}/></button></div>
    <button className={`select-hit ${selected ? 'checked' : ''}`} aria-label={`${selected ? 'Deselect' : 'Select'} ${book.title}`} aria-pressed={selected} onClick={onToggle}>{selected && <Check size={12}/>}</button>
    {menu && <ContextMenu anchor={menu} onClose={onMenuClose} items={[{ label: 'Read', icon: <BookOpen/>, onSelect: onPreview, disabled: !canRead }, { label: 'Edit details', icon: <Pencil/>, onSelect: onEdit }, { label: deviceMenuLabel(canSend, sendLabel), icon: <Send/>, onSelect: onSend, disabled: !canSend }, { label: 'Remove from library', icon: <Trash2/>, onSelect: onDelete, danger: true }]}/>}
  </article>
}

function deviceMenuLabel(canSend: boolean, label: string) { return canSend || label === 'Connect a reader to send' || label === 'Set transfer settings first' || label === 'Transfer in progress' ? label : 'Set transfer settings first' }

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


const unknownFilter = '__unknown__'
function matchesFilter(values: string[] | undefined, selected: string) {
  if (!selected) return true
  const present = (values ?? []).some(value => value.trim())
  return selected === unknownFilter ? !present : (values ?? []).includes(selected)
}
function FilterTag({ label, value, onRemove }: { label: string; value: string; onRemove: () => void }) { return <span className="filter-tag"><span>{label}: {value}</span><button aria-label={`Remove ${label} filter`} onClick={onRemove}><X size={12}/></button></span> }
function FilterPicker({ authorFilter, genreFilter, languageFilter, metadata, setAuthorFilter, setGenreFilter, setLanguageFilter }: { authorFilter: string; genreFilter: string; languageFilter: string; metadata: { authors: { values: string[]; missing: boolean }; genres: { values: string[]; missing: boolean }; languages: { values: string[]; missing: boolean } }; setAuthorFilter: (value: string) => void; setGenreFilter: (value: string) => void; setLanguageFilter: (value: string) => void }) {
  const [open, setOpen] = useState(false)
  const root = useRef<HTMLDivElement>(null), trigger = useRef<HTMLButtonElement>(null)
  useEffect(() => {
    if (!open) return
    const outside = (e: PointerEvent) => { if (!root.current?.contains(e.target as Node) && !(e.target as HTMLElement).closest?.('[data-dropdown-select-menu]')) setOpen(false) }
    const escape = (e: KeyboardEvent) => { if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); setOpen(false); trigger.current?.focus() } }
    document.addEventListener('pointerdown', outside); document.addEventListener('keydown', escape)
    return () => { document.removeEventListener('pointerdown', outside); document.removeEventListener('keydown', escape) }
  }, [open])
  const active = [authorFilter, genreFilter, languageFilter].filter(Boolean).length
  return <div className="filter-picker" ref={root}><button ref={trigger} className={`filter-trigger ${active ? 'has-filters' : ''}`} aria-label={`Filters${active ? ` (${active} active)` : ''}`} title="Filter books" aria-haspopup="dialog" aria-expanded={open} onClick={() => setOpen(v => !v)}><Settings2 size={15}/> Filters{active > 0 && <span>{active}</span>}</button>{open && <div className="filter-popover" role="dialog" aria-label="Filter books"><FilterSelect label="Author" value={authorFilter} values={metadata.authors.values} hasUnknown={metadata.authors.missing} onChange={setAuthorFilter}/><FilterSelect label="Genre" value={genreFilter} values={metadata.genres.values} hasUnknown={metadata.genres.missing} onChange={setGenreFilter}/><FilterSelect label="Language" value={languageFilter} values={metadata.languages.values} hasUnknown={metadata.languages.missing} onChange={setLanguageFilter}/></div>}</div>
}
function FilterSelect({ label, value, values, hasUnknown, onChange }: { label: string; value: string; values: string[]; hasUnknown: boolean; onChange: (value: string) => void }) {
  const plural = label === 'Genre' ? 'genres' : `${label.toLowerCase()}s`
  const options = [{ value: '', label: `All ${plural}` }, ...values.map(item => ({ value: item, label: label === 'Language' ? languageLabel(item) : item })), ...(hasUnknown ? [{ value: unknownFilter, label: `Unknown ${label.toLowerCase()}` }] : [])]
  return <div className="filter-select"><span>{label}</span><DropdownSelect label={`Filter by ${label.toLowerCase()}`} value={value} options={options} onChange={onChange}/></div>
}
function languageLabel(code: string) {
  try { return new Intl.DisplayNames([navigator.language || 'en'], { type: 'language' }).of(code) || code }
  catch { return code }
}

function readerSetting(key: string, fallback: string) {
  try { return localStorage.getItem(key) || fallback } catch { return fallback }
}

const readerPreviewParagraphs = [
  'The rain had stopped just before first light. Water clung to the garden wall, turning every stone dark and bright at once, while the old house listened to the quiet as if it had been waiting all night for the world to speak again.',
  'Mara found the book where she had left it: open on the kitchen table, its pages held down by a smooth river stone. She could not remember bringing the stone inside. It was warm in her palm, though the morning air still carried the chill of the storm.',
  'A narrow line of ink crossed the page beneath the last sentence. It had not been there yesterday. The letters leaned toward the edge of the paper, pointing west, toward the hills she could see only when the clouds lifted.',
  'She read the sentence twice. Beyond the orchard, where the old footpath disappeared beneath bracken, a bell was ringing. No one in the village owned a bell like that. No one, as far as she knew, had heard it in years.',
  'Mara pulled on her coat and filled a small flask at the tap. She packed an apple, a pencil, and the folded map from the drawer. The book stayed open on the table. Its pages stirred when she passed, although every window was shut.',
  'At the edge of the orchard she stopped to look back. The house stood quiet among its apple trees, its windows pale with reflected sky. For a moment she felt the familiar tug of turning home, of putting the kettle on and deciding the bell had been nothing.',
  'Then it rang again. The sound came from the ridge, clear and patient, and the book in her bag seemed to grow heavier. She stepped through the gap in the wall and followed the path into the woods.',
  'The first mile was easy. Rain had softened the ground but not washed the track away. Ferns brushed her sleeves, and the trees opened now and then to show a strip of silver sky. She kept the map folded in one hand.',
  'At the stream the path split. One branch crossed a wooden bridge; the other wound between two standing stones. The map showed neither. Mara took out the book and found a small drawing in its margin: two stones, a stream, and a mark like a star.',
  'She crossed the bridge. On the far bank, the woods changed. The trees grew farther apart, and their trunks carried shallow carvings filled with pale moss. Each mark resembled a letter, but none belonged to any alphabet she knew.',
  'By midday the clouds had broken into long bright ribbons. Mara sat beneath an ash tree and ate her apple. She wrote down what she had seen, including the bell, the stones, and the sentence in the book. Writing made the day feel ordinary again.',
  'When she opened the book to check the map, a second line had appeared below the first. It named a place: the house with the blue door. Mara knew it from the village stories, though no one agreed where it stood.',
  'The bell rang from somewhere ahead. This time there was an answer, a faint chime from behind her. She turned, but the trail was empty. The trees shifted in the wind and the light changed across the moss.',
  'She continued toward the ridge, counting each turn and marking the map. A good map, her father used to say, was less a picture of the world than a record of the choices you had made inside it.',
  'At the top, she found a low stone wall and a gate that opened onto a field. Beyond it stood a small house with a blue door. Smoke curled from its chimney, though the roof was furred with ivy and the windows looked dark.',
  'Mara reached for the latch. The book opened by itself to a clean white page. She waited for new ink, but nothing appeared. Then someone inside the house laughed, and the door swung inward.',
]

function ReaderView({ book, preview, loading, error, chapterIndex, setChapterIndex, search, findRef, turnRef }: { book: Book; preview: BookPreview | null; loading: boolean; error: string; chapterIndex: number; setChapterIndex: (index: number) => void; search: string; findRef: React.MutableRefObject<() => void>; turnRef: React.MutableRefObject<(direction: -1 | 1) => void> }) {
  const selectedFile = preview?.fileID ? book.files?.find(file => file.id === preview.fileID) : undefined
  const engineMode = !bridge.preview && !!selectedFile
  const fixedLayout = !!selectedFile && ['pdf', 'cbz', 'cbr'].includes(selectedFile.format.toLowerCase())
  const engine = useRef<ReaderSurfaceHandle>(null)
  const [engineSections, setEngineSections] = useState<ReaderSection[]>([])
  const [engineChapterIndex, setEngineChapterIndex] = useState(0)
  const [engineProgress, setEngineProgress] = useState(0)
  const [engineLoading, setEngineLoading] = useState(false)
  const [engineError, setEngineError] = useState('')
  const [fit, setFit] = useState<ReaderSettings['fit']>(() => readerSetting('attune.reader.fit', 'page') as ReaderSettings['fit'])
  const chapters = engineMode ? engineSections.map(section => ({ id: section.id, title: section.title, href: '' })) : preview?.chapters ?? []
  const currentChapterIndex = engineMode ? engineChapterIndex : chapterIndex
  const active = chapters[currentChapterIndex]
  const frame = useRef<HTMLIFrameElement>(null)
  const panel = useRef<HTMLElement>(null)
  const suppressPageClickUntil = useRef(0)
  const wheelGesture = useRef({ total: 0, direction: 0, turned: false, lastAt: 0 })
  const pagination = useRef<ReaderPagination | null>(null)
  const loadedDocument = useRef<Document | null>(null)
  const pendingChapterPosition = useRef<number | null>(null)
  const [frameVersion, setFrameVersion] = useState(0)
  const saveTimer = useRef<number | undefined>(undefined)
  const [tocOpen, setTocOpen] = useState(true)
  const [fullscreen, setFullscreen] = useState(false)
  const [chromeVisible, setChromeVisible] = useState(true)
  const chromeTimer = useRef<number | undefined>(undefined)
  const chromeHovered = useRef(false)
  const [progress, setProgress] = useState({ chapterIndex: 0, position: 0 })
  const [savedPosition, setSavedPosition] = useState(0)
  const [bookmarks, setBookmarks] = useState<import('./types').ReadingBookmark[]>([])
  const [annotations, setAnnotations] = useState<ReaderAnnotation[]>([])
  const [tocTab, setTocTab] = useState<'contents' | 'bookmarks' | 'annotations'>('contents')
  const [font, setFont] = useState(() => readerSetting('attune.reader.font', 'serif'))
  const [fontSize, setFontSize] = useState(() => Math.min(30, Math.max(14, Number(readerSetting('attune.reader.size', '19')) || 19)))
  const [lineHeight, setLineHeight] = useState(() => Math.min(2.4, Math.max(1.3, Number(readerSetting('attune.reader.spacing', '1.8')) || 1.8)))
  const [textMenuOpen, setTextMenuOpen] = useState(false)
  const [textMenuPosition, setTextMenuPosition] = useState({ left: 0, top: 0 })
  const textMenuTrigger = useRef<HTMLButtonElement>(null)
  const textMenuPanel = useRef<HTMLDivElement>(null)
  const [theme, setTheme] = useState(() => readerSetting('attune.reader.theme', 'light'))
  const [layout, setLayout] = useState(() => readerSetting('attune.reader.layout', 'single'))
  const [sidePadding, setSidePadding] = useState(() => Math.min(96, Math.max(24, Number(readerSetting('attune.reader.padding', '48')) || 48)))
  const readerSettings = useMemo<ReaderSettings>(() => ({ font: font as ReaderSettings['font'], fontSize, lineHeight, padding: sidePadding, layout: layout as ReaderSettings['layout'], theme: theme as ReaderSettings['theme'], fit }), [font, fontSize, lineHeight, sidePadding, layout, theme, fit])
  const readerLoading = engineMode ? engineLoading : loading
  const readerError = engineMode ? engineError || error : error
  const currentProgress = engineMode ? engineProgress : progress.chapterIndex === currentChapterIndex ? progress.position : 0
  const safeHref = active?.href.startsWith('/preview/') && !active.href.startsWith('//') ? active.href : ''
  const previewHtml = bridge.preview ? `<!doctype html><html><head><meta name="viewport" content="width=device-width, initial-scale=1"><style>body{font:19px/1.8 Georgia,serif;max-width:620px;margin:50px auto;padding:0 28px;color:#34343a}h1{font:600 27px/1.25 Georgia,serif}p{margin:1.1em 0}</style></head><body><h1>${escapeHTML(active?.title ?? 'A quiet beginning')}</h1>${readerPreviewParagraphs.map(paragraph => `<p>${escapeHTML(paragraph)}</p>`).join('')}<p>${escapeHTML(book.title)} stayed with her as the path curved toward the ridge. What waited beyond it would have to become a story of its own.</p></body></html>` : undefined
  const positionInFrame = (documentRef = frame.current?.contentDocument) => {
    const doc = documentRef, scroller = doc?.scrollingElement
    if (!doc || !scroller) return 0
    if (pagination.current?.doc === doc) return pagination.current.position()
    return Math.max(0, Math.min(1, scroller.scrollTop / Math.max(1, scroller.scrollHeight - doc.documentElement.clientHeight)))
  }
  const save = (index = chapterIndex, position = positionInFrame()) => {
    setProgress({ chapterIndex: index, position })
    window.clearTimeout(saveTimer.current)
    saveTimer.current = window.setTimeout(() => { void bridge.saveReadingProgress(book.id, index, position).catch(() => {}) }, 500)
  }
  useEffect(() => {
    let live = true
    void bridge.getReadingProgress(book.id).then(saved => { if (live) { setProgress(saved); if (saved.chapterIndex === chapterIndex) setSavedPosition(saved.position) } }).catch(() => {})
    void bridge.getReadingBookmarks(book.id).then(items => { if (live) setBookmarks(items) }).catch(() => {})
    return () => { live = false; window.clearTimeout(saveTimer.current) }
  }, [book.id])
  useEffect(() => {
    let live = true
    setAnnotations([])
    if (selectedFile) void bridge.getReaderAnnotations(book.id, selectedFile.id).then(items => { if (live) setAnnotations(items) }).catch(() => {})
    return () => { live = false }
  }, [book.id, selectedFile?.id])
  useEffect(() => {
    if (!engineMode || !engineSections.length || !annotations.some(item => item.cfi)) return
    let live = true
    void Promise.all(annotations.map(async item => {
      if (!item.cfi) return item
      const resolved = await engine.current?.resolve({ version: 1, kind: 'cfi', value: item.cfi, progress: 0 }).catch(() => false)
      return { ...item, status: resolved ? 'placed' as const : 'unplaced' as const }
    })).then(next => {
      if (live && next.some((item, index) => item.status !== annotations[index]?.status)) setAnnotations(next)
    })
    return () => { live = false }
  }, [engineMode, engineSections, annotations])
  useEffect(() => {
    setEngineSections([])
    setEngineChapterIndex(0)
    setEngineProgress(0)
    setEngineError('')
  }, [book.id, preview?.fileID])
  useEffect(() => {
    const onFullscreen = () => setFullscreen(document.fullscreenElement === panel.current)
    document.addEventListener('fullscreenchange', onFullscreen)
    return () => document.removeEventListener('fullscreenchange', onFullscreen)
  }, [])
  useEffect(() => {
    if (bridge.preview) return
    const syncFullscreen = () => {
      const state = window.runtime?.WindowIsFullscreen?.()
      if (state) void state.then(setFullscreen).catch(() => {})
    }
    window.addEventListener('resize', syncFullscreen)
    return () => window.removeEventListener('resize', syncFullscreen)
  }, [])
  useEffect(() => {
    const onEscape = (event: KeyboardEvent) => {
      if (event.key !== 'Escape' || !fullscreen || textMenuOpen || document.querySelector('.dropdown-select-menu')) return
      event.preventDefault()
      event.stopImmediatePropagation()
      if (bridge.preview) void document.exitFullscreen?.()
      else window.runtime?.WindowUnfullscreen?.()
      setFullscreen(false)
    }
    window.addEventListener('keydown', onEscape, true)
    return () => window.removeEventListener('keydown', onEscape, true)
  }, [fullscreen, textMenuOpen])
  useEffect(() => {
    if (!textMenuOpen) return
    const onPointerDown = (event: PointerEvent) => {
      const target = event.target as HTMLElement
      if (textMenuTrigger.current?.contains(target) || textMenuPanel.current?.contains(target) || target.closest('.dropdown-select-menu')) return
      setTextMenuOpen(false)
    }
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !document.querySelector('.dropdown-select-menu')) { event.preventDefault(); event.stopPropagation(); setTextMenuOpen(false) }
    }
    document.addEventListener('pointerdown', onPointerDown)
    document.addEventListener('keydown', onKeyDown)
    return () => { document.removeEventListener('pointerdown', onPointerDown); document.removeEventListener('keydown', onKeyDown) }
  }, [textMenuOpen])
  useEffect(() => {
    window.clearTimeout(chromeTimer.current)
    if (!fullscreen) { setChromeVisible(true); return }
    setChromeVisible(false)
    const onMove = (event: PointerEvent) => {
      if (event.clientY > 76) return
      setChromeVisible(true)
      window.clearTimeout(chromeTimer.current)
      if (!chromeHovered.current) chromeTimer.current = window.setTimeout(() => setChromeVisible(false), 1800)
    }
    window.addEventListener('pointermove', onMove)
    return () => { window.removeEventListener('pointermove', onMove); window.clearTimeout(chromeTimer.current) }
  }, [fullscreen])
  useEffect(() => {
    try { localStorage.setItem('attune.reader.font', font); localStorage.setItem('attune.reader.size', String(fontSize)); localStorage.setItem('attune.reader.spacing', String(lineHeight)); localStorage.setItem('attune.reader.theme', theme); localStorage.setItem('attune.reader.layout', layout); localStorage.setItem('attune.reader.padding', String(sidePadding)); localStorage.setItem('attune.reader.fit', fit) } catch { /* Current controls remain available for the session. */ }
  }, [font, fontSize, lineHeight, theme, layout, sidePadding, fit])
  const applyReaderStyles = (restorePosition?: number) => {
    const doc = frame.current?.contentDocument
    if (!doc?.head || !doc.body || doc !== loadedDocument.current) return
    const position = restorePosition ?? positionInFrame(doc)
    doc.body.style.removeProperty('transform')
    doc.body.style.removeProperty('transition')
    doc.getElementById('attune-reader-style')?.remove()
    const style = doc.createElement('style')
    style.id = 'attune-reader-style'
    const colors = theme === 'dark' ? ['#232428', '#e6e6e8', '#89a5ff'] : theme === 'sepia' ? ['#f2eadb', '#40372c', '#715e48'] : theme === 'gray' ? ['#bcbcbc', '#303030', '#4e4e4e'] : theme === 'charcoal' ? ['#303030', '#ededed', '#c2c2c2'] : ['#fff', '#25262a', '#5278dc']
    const families: Record<string, string> = { serif: 'Georgia, "Times New Roman", serif', book: 'Palatino, "Book Antiqua", serif', sans: 'system-ui, -apple-system, sans-serif', mono: 'ui-monospace, monospace' }
    const paged = layout !== 'continuous'
    const spread = layout === 'two-page'
    const padding = Math.min(sidePadding, doc.documentElement.clientWidth / (spread ? 8 : 4))
    // Each column plus its gutter is exactly one leaf; a spread always advances one viewport.
    const pageRules = paged ? `html{height:100%!important;overflow:hidden!important;touch-action:pan-y}body{box-sizing:border-box!important;height:100vh!important;width:100%!important;min-width:0!important;max-width:none!important;margin:0!important;padding:24px ${padding}px!important;column-count:${spread ? 2 : 1}!important;column-width:auto!important;column-gap:${padding * 2}px!important;column-fill:auto!important;overflow:visible!important;direction:ltr!important;writing-mode:horizontal-tb!important;}` : `html{height:auto;overflow-y:auto!important;overflow-x:hidden!important}body{box-sizing:border-box!important;width:auto!important;max-width:760px!important;height:auto!important;margin:0 auto!important;padding:52px ${sidePadding}px 72px!important;column-count:auto!important;column-width:auto!important;column-gap:normal!important;}`
    style.textContent = `html,body{background:${colors[0]}!important;color:${colors[1]}!important}body,body *{font-family:${families[font]}!important;color:${colors[1]}!important}body{font-size:${fontSize}px!important;line-height:${lineHeight}!important}${pageRules}p,li,blockquote{line-height:${lineHeight}!important}a{color:${colors[2]}!important}img,svg,video{max-width:100%;max-height:100%;height:auto}pre{white-space:pre-wrap;overflow-wrap:anywhere}`
    doc.head.appendChild(style)
    if (paged) pagination.current = createReaderPagination(doc, spread ? 2 : 1, padding, position)
    else {
      pagination.current = null
      doc.scrollingElement?.scrollTo(0, position * Math.max(0, doc.scrollingElement.scrollHeight - doc.documentElement.clientHeight))
    }
  }
  useEffect(() => {
    applyReaderStyles()
    const iframe = frame.current
    if (!iframe) return
    const observer = new ResizeObserver(() => applyReaderStyles())
    observer.observe(iframe)
    const doc = iframe.contentDocument
    let live = true
    void doc?.fonts.ready.then(() => { if (live) applyReaderStyles() })
    const onImageLoad = (event: Event) => { if ((event.target as Element | null)?.tagName === 'IMG') applyReaderStyles() }
    doc?.addEventListener('load', onImageLoad, true)
    return () => { live = false; observer.disconnect(); doc?.removeEventListener('load', onImageLoad, true) }
  }, [frameVersion, font, fontSize, lineHeight, theme, loading, layout, sidePadding])
  useEffect(() => {
    const doc = frame.current?.contentDocument, scroller = doc?.scrollingElement
    if (!doc || !scroller || loading || doc !== loadedDocument.current) return
    const onScroll = () => save(chapterIndex, positionInFrame())
    doc.addEventListener('scroll', onScroll, { passive: true })
    const initialPagination = pagination.current
    return () => {
      doc.removeEventListener('scroll', onScroll)
      const position = pagination.current?.doc === doc ? pagination.current.position() : initialPagination?.doc === doc ? initialPagination.position() : positionInFrame(doc)
      window.clearTimeout(saveTimer.current)
      void bridge.saveReadingProgress(book.id, chapterIndex, position).catch(() => {})
    }
  }, [frameVersion, chapterIndex, book.id, loading])
  useEffect(() => {
    const doc = frame.current?.contentDocument
    if (!doc || progress.chapterIndex !== chapterIndex) return
    if (pagination.current?.doc === doc) pagination.current.goTo(Math.round(savedPosition * (pagination.current.count - 1)), false)
    else if (doc.scrollingElement) doc.scrollingElement.scrollTop = savedPosition * Math.max(0, doc.scrollingElement.scrollHeight - doc.documentElement.clientHeight)
  }, [savedPosition, frameVersion])
  const turnPage = (direction: -1 | 1) => {
    if (engineMode) { if (direction < 0) void engine.current?.previous(); else void engine.current?.next(); return }
    const doc = frame.current?.contentDocument, scroller = doc?.scrollingElement
    if (!doc || !scroller) return
    const pages = pagination.current
    if (pages?.doc === doc) {
      const next = pages.index + direction
      if (next >= 0 && next < pages.count) { pages.goTo(next); save(); return }
      const chapter = chapterIndex + direction
      if (chapter >= 0 && chapter < chapters.length) { pendingChapterPosition.current = direction < 0 ? 1 : 0; setChapterIndex(chapter) }
      return
    }
    const offset = scroller.scrollTop
    const max = scroller.scrollHeight - doc.documentElement.clientHeight
    const atEdge = direction < 0 ? offset <= 2 : offset >= max - 2
    if (atEdge) { if (direction < 0 && chapterIndex > 0) setChapterIndex(chapterIndex - 1); else if (direction > 0 && chapterIndex < chapters.length - 1) setChapterIndex(chapterIndex + 1); return }
    scroller.scrollBy({ top: direction * doc.documentElement.clientHeight, behavior: 'smooth' })
  }
  const navigateSection = (index: number) => {
    if (engineMode) { void engine.current?.navigate(index); return }
    setChapterIndex(index)
  }
  turnRef.current = turnPage
  useEffect(() => {
    const doc = frame.current?.contentDocument
    if (!doc) return
    const onKeyDown = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null
      if (target && (target.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName))) return
      if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') {
        event.preventDefault()
        turnRef.current(event.key === 'ArrowLeft' ? -1 : 1)
      }
    }
    const onPointerMove = (event: PointerEvent) => {
      if (!fullscreen || event.clientY > 68) return
      setChromeVisible(true)
      window.clearTimeout(chromeTimer.current)
      if (!chromeHovered.current) chromeTimer.current = window.setTimeout(() => setChromeVisible(false), 1800)
    }
    doc.addEventListener('keydown', onKeyDown)
    doc.addEventListener('pointermove', onPointerMove)
    return () => { doc.removeEventListener('keydown', onKeyDown); doc.removeEventListener('pointermove', onPointerMove) }
  }, [frameVersion, loading, turnRef, fullscreen, layout])
  useEffect(() => {
    if (layout === 'continuous' || loading) return
    const doc = frame.current?.contentDocument
    if (!doc) return
    let startX: number | null = null
    let startY = 0
    const onPointerDown = (event: PointerEvent) => {
      if (event.button !== 0 || (event.target as HTMLElement | null)?.closest('a,button,input,select,textarea,video')) return
      startX = event.clientX
      startY = event.clientY
    }
    const onPointerUp = (event: PointerEvent) => {
      if (startX === null) return
      const deltaX = event.clientX - startX
      const deltaY = event.clientY - startY
      startX = null
      if (Math.abs(deltaX) >= 64 && Math.abs(deltaX) > Math.abs(deltaY) * 1.3) {
        suppressPageClickUntil.current = Date.now() + 350
        doc.getSelection()?.removeAllRanges()
        turnRef.current(deltaX < 0 ? 1 : -1)
      }
    }
    const onPointerCancel = () => { startX = null }
    const onClick = (event: MouseEvent) => {
      if (Date.now() < suppressPageClickUntil.current) { event.preventDefault(); return }
      const target = event.target as HTMLElement | null
      if (target?.closest('a,button,input,select,textarea,video') || doc.getSelection()?.toString()) return
      turnRef.current(event.clientX < doc.documentElement.clientWidth / 2 ? -1 : 1)
    }
    const onWheel = (event: WheelEvent) => {
      if (event.ctrlKey) return
      event.preventDefault()
      const delta = Math.abs(event.deltaX) > Math.abs(event.deltaY) ? event.deltaX : event.deltaY
      const direction = Math.sign(delta)
      const now = Date.now()
      const gesture = wheelGesture.current
      if (now - gesture.lastAt > 200 || direction !== gesture.direction) { gesture.total = 0; gesture.turned = false }
      gesture.lastAt = now
      gesture.direction = direction
      gesture.total += Math.abs(delta) * (event.deltaMode === 1 ? 16 : event.deltaMode === 2 ? doc.documentElement.clientHeight : 1)
      if (!gesture.turned && gesture.total >= 32 && direction) { gesture.turned = true; turnRef.current(direction > 0 ? 1 : -1) }
    }
    doc.addEventListener('pointerdown', onPointerDown)
    doc.addEventListener('pointerup', onPointerUp)
    doc.addEventListener('pointercancel', onPointerCancel)
    doc.addEventListener('click', onClick)
    doc.addEventListener('wheel', onWheel, { passive: false })
    return () => { doc.removeEventListener('pointerdown', onPointerDown); doc.removeEventListener('pointerup', onPointerUp); doc.removeEventListener('pointercancel', onPointerCancel); doc.removeEventListener('click', onClick); doc.removeEventListener('wheel', onWheel) }
  }, [frameVersion, loading, layout, turnRef])
  const addBookmark = async () => {
    if (!active) return
    const position = engineMode ? currentProgress : positionInFrame()
    try {
      const bookmark = await bridge.addReadingBookmark(book.id, currentChapterIndex, position)
      setBookmarks(items => [bookmark, ...items])
      setTocTab('bookmarks')
    } catch { /* Keep the reader usable if bookmark storage fails. */ }
  }
  const openBookmark = (bookmark: import('./types').ReadingBookmark) => {
    navigateSection(bookmark.chapterIndex)
    setSavedPosition(bookmark.position)
    setProgress({ chapterIndex: bookmark.chapterIndex, position: bookmark.position })
    void bridge.saveReadingProgress(book.id, bookmark.chapterIndex, bookmark.position).catch(() => {})
  }
  useEffect(() => {
    findRef.current = () => {
      if (engineMode) {
        if (!search.trim()) return
        void engine.current?.search(search).then(hits => {
          if (hits[0]) {
            engine.current?.goToSearchHit(hits[0])
            setTocTab('contents')
          }
        }).catch(() => {})
        return
      }
      const doc = frame.current?.contentDocument
      const needle = search.trim().toLocaleLowerCase()
      if (!doc?.body || !needle) return
      const walker = doc.createTreeWalker(doc.body, NodeFilter.SHOW_TEXT)
      const nodes: Text[] = []
      while (walker.nextNode()) {
        const node = walker.currentNode as Text
        if (!node.parentElement?.closest('script,style,noscript')) nodes.push(node)
      }
      const selection = doc.getSelection()
      let startIndex = 0
      let startOffset = 0
      if (selection && !selection.isCollapsed && selection.toString().toLocaleLowerCase() === needle) {
        const index = nodes.indexOf(selection.focusNode as Text)
        if (index >= 0) { startIndex = index; startOffset = selection.focusOffset }
      }
      for (let pass = 0; pass < 2; pass++) {
        for (let index = pass ? 0 : startIndex; index < (pass ? startIndex + 1 : nodes.length); index++) {
          const node = nodes[index]
          const text = node.nodeValue ?? ''
          const offset = pass ? 0 : index === startIndex ? startOffset : 0
          const found = text.toLocaleLowerCase().indexOf(needle, offset)
          if (found < 0) continue
          const range = doc.createRange()
          range.setStart(node, found)
          range.setEnd(node, found + search.trim().length)
          selection?.removeAllRanges()
          selection?.addRange(range)
          const pages = pagination.current
          if (pages?.doc === doc) {
            const x = range.getBoundingClientRect().left + pages.index * pages.width
            pages.goTo(Math.floor(x / pages.width))
            save()
          } else node.parentElement?.scrollIntoView({ block: 'center', behavior: 'smooth' })
          return
        }
      }
    }
  }, [search, findRef, active?.href, engineMode])
  const onFrameLoad = () => {
    loadedDocument.current = frame.current?.contentDocument ?? null
    const position = pendingChapterPosition.current ?? (progress.chapterIndex === chapterIndex ? savedPosition : 0)
    pendingChapterPosition.current = null
    applyReaderStyles(position)
    setSavedPosition(position)
    setFrameVersion(version => version + 1)
    setProgress({ chapterIndex, position: positionInFrame() })
  }
  const percent = Math.round(currentProgress * 100)
  return <section ref={panel} className={`reader-view reader-theme-${theme} ${fullscreen ? 'reader-fullscreen' : ''}`} aria-label={`Reading ${book.title}`} onPointerMove={event => { if (fullscreen && event.clientY <= 76) { setChromeVisible(true); window.clearTimeout(chromeTimer.current); if (!chromeHovered.current) chromeTimer.current = window.setTimeout(() => setChromeVisible(false), 1800) } }}>
    {fullscreen && <div className="reader-chrome-reveal" aria-hidden="true" onPointerEnter={() => { setChromeVisible(true); window.clearTimeout(chromeTimer.current); chromeTimer.current = window.setTimeout(() => setChromeVisible(false), 1800) }}/>}
    <div className={`reader-subbar ${fullscreen ? 'fullscreen-chrome' : ''} ${chromeVisible ? 'chrome-visible' : ''}`} onPointerEnter={() => { chromeHovered.current = true; window.clearTimeout(chromeTimer.current); if (fullscreen) setChromeVisible(true) }} onPointerLeave={() => { chromeHovered.current = false; if (fullscreen) chromeTimer.current = window.setTimeout(() => setChromeVisible(false), 700) }}>
      <button className="icon-button" aria-label={tocOpen ? 'Hide reader sidebar' : 'Show reader sidebar'} title="Contents and bookmarks" onClick={() => setTocOpen(open => !open)}>{tocOpen ? <PanelLeftClose size={17}/> : <PanelLeftOpen size={17}/>}</button>
      <span className="reader-chapter-title">{active?.title || 'Contents'}</span>
      <div className="reader-controls">
        <div className="reader-layout-switch" role="group" aria-label="Reading layout">
          <button className={`icon-button ${layout === 'continuous' ? 'selected' : ''}`} aria-label="Continuous layout" title="Continuous layout" aria-pressed={layout === 'continuous'} onClick={() => setLayout('continuous')}><AlignLeft size={16}/></button>
          <button className={`icon-button ${layout === 'single' ? 'selected' : ''}`} aria-label="Single-page layout" title="Single-page layout" aria-pressed={layout === 'single'} onClick={() => setLayout('single')}><BookOpen size={16}/></button>
          <button className={`icon-button ${layout === 'two-page' ? 'selected' : ''}`} aria-label="Two-page layout" title="Two-page layout" aria-pressed={layout === 'two-page'} onClick={() => setLayout('two-page')}><Columns2 size={16}/></button>
        </div>
        <button className="icon-button" title="Add bookmark" aria-label="Add bookmark" onClick={() => void addBookmark()}><BookmarkPlus size={16}/></button>
        {!fixedLayout ? <div className="reader-text-menu">
          <button ref={textMenuTrigger} type="button" className="reader-dropdown reader-text-trigger" aria-label="Text settings" aria-haspopup="dialog" aria-expanded={textMenuOpen} onClick={event => { if (textMenuOpen) { setTextMenuOpen(false); return }; const rect = event.currentTarget.getBoundingClientRect(); setTextMenuPosition({ left: Math.max(8, Math.min(window.innerWidth - 296, rect.right - 280)), top: Math.max(8, Math.min(window.innerHeight - 292, rect.bottom + 8)) }); setTextMenuOpen(true) }}><Type size={15}/><span>Text</span><ChevronDown size={14}/></button>
          {textMenuOpen && createPortal(<div ref={textMenuPanel} className={`reader-text-menu-panel reader-theme-${theme}`} role="dialog" aria-label="Text settings" style={{ left: textMenuPosition.left, top: textMenuPosition.top }}>
            <label className="reader-text-control"><span>Font size <output>{fontSize}px</output></span><input aria-label="Font size" type="range" min="14" max="30" step="1" value={fontSize} style={{ '--range-progress': `${((fontSize - 14) / 16) * 100}%` } as CSSProperties} onChange={e => setFontSize(Number(e.target.value))}/></label>
            <div className="reader-text-family"><span>Font family</span><div className="reader-dropdown"><DropdownSelect label="Typeface" value={font} onChange={setFont} menuClassName={`reader-dropdown-menu reader-theme-${theme}`} options={[{ value: 'serif', label: 'Serif' }, { value: 'book', label: 'Book' }, { value: 'sans', label: 'Sans' }, { value: 'mono', label: 'Mono' }]}/></div></div>
            <label className="reader-text-control"><span>Line spacing <output>{lineHeight.toFixed(1)}×</output></span><input aria-label="Line spacing" type="range" min="1.3" max="2.4" step="0.1" value={lineHeight} style={{ '--range-progress': `${((lineHeight - 1.3) / 1.1) * 100}%` } as CSSProperties} onChange={e => setLineHeight(Number(e.target.value))}/></label>
            <label className="reader-text-control"><span>Text padding <output>{sidePadding}px</output></span><input aria-label="Text side padding" type="range" min="24" max="96" step="4" value={sidePadding} style={{ '--range-progress': `${((sidePadding - 24) / 72) * 100}%` } as CSSProperties} onChange={e => setSidePadding(Number(e.target.value))}/></label>
          </div>, fullscreen && panel.current ? panel.current : document.body)}
        </div> : <div className="reader-dropdown reader-fit-dropdown"><DropdownSelect label="Page fit" value={fit} onChange={value => setFit(value as ReaderSettings['fit'])} options={[{ value: 'page', label: 'Fit page' }, { value: 'width', label: 'Fit width' }]}/></div>}
        <div className="reader-dropdown reader-theme-dropdown"><DropdownSelect label="Reading theme" value={theme} onChange={setTheme} menuClassName={`reader-dropdown-menu reader-theme-${theme}`} options={[{ value: 'light', label: 'Light' }, { value: 'sepia', label: 'Sepia' }, { value: 'gray', label: 'Gray' }, { value: 'charcoal', label: 'Charcoal' }, { value: 'dark', label: 'Dark' }]}/></div>
        <button className="icon-button" title={fullscreen ? 'Exit full screen' : 'Full screen'} aria-label={fullscreen ? 'Exit full screen' : 'Full screen'} onClick={() => { if (bridge.preview) { if (document.fullscreenElement) void document.exitFullscreen(); else void panel.current?.requestFullscreen?.() } else if (fullscreen) { window.runtime?.WindowUnfullscreen?.(); setFullscreen(false) } else if (window.runtime?.WindowFullscreen) { window.runtime.WindowFullscreen(); setFullscreen(true) } }}>{fullscreen ? <Minimize2 size={16}/> : <Maximize2 size={16}/>}</button>
      </div>
    </div>
    <div className={`reader-layout ${tocOpen ? '' : 'toc-hidden'}`}>
      {tocOpen && <nav className="reader-toc" aria-label="Reader navigation">
        <div className="reader-toc-tabs"><button style={{ width: '33.333%' }} className={tocTab === 'contents' ? 'active' : ''} onClick={() => setTocTab('contents')}>Contents</button><button style={{ width: '33.333%' }} className={tocTab === 'bookmarks' ? 'active' : ''} onClick={() => setTocTab('bookmarks')}>Bookmarks {bookmarks.length > 0 && <span>{bookmarks.length}</span>}</button><button style={{ width: '33.333%' }} className={tocTab === 'annotations' ? 'active' : ''} onClick={() => setTocTab('annotations')}>Notes {annotations.length > 0 && <span>{annotations.length}</span>}</button></div>
        {tocTab === 'contents' ? chapters.map((chapter, index) => <button key={chapter.id} className={index === currentChapterIndex ? 'active' : ''} onClick={() => navigateSection(index)}>{chapter.title || `Chapter ${index + 1}`}</button>) : tocTab === 'bookmarks' ? bookmarks.length ? bookmarks.map(item => <div className="reader-bookmark" key={item.id}><button onClick={() => openBookmark(item)}><Bookmark size={14}/><span>{chapters[item.chapterIndex]?.title || `Chapter ${item.chapterIndex + 1}`}<small>{Math.round(item.position * 100)}%</small></span></button><button className="reader-bookmark-delete" aria-label="Delete bookmark" title="Delete bookmark" onClick={() => { void bridge.deleteReadingBookmark(book.id, item.id).then(() => setBookmarks(items => items.filter(candidate => candidate.id !== item.id))).catch(() => {}) }}><Trash size={14}/></button></div>) : <p className="reader-empty-bookmarks">No bookmarks yet. Add one from the toolbar while reading.</p> : annotations.length ? annotations.map(item => <div className="reader-bookmark reader-annotation" key={item.id}><button disabled={!engineMode || (!item.cfi && !item.quote)} title={!item.cfi && !item.quote ? 'This note has no text anchor' : 'Open this note in the book'} onClick={() => { if (!engineMode) return; void (async () => { let found = false; if (item.cfi) found = await engine.current?.restore({ version: 1, kind: 'cfi', value: item.cfi, progress: 0 }) ?? false; else if (item.quote) { const hits = await engine.current?.search(item.quote.slice(0, 500)) ?? []; if (hits.length === 1) { engine.current?.goToSearchHit(hits[0]); found = true } }; if (found) setAnnotations(items => items.map(candidate => candidate.id === item.id ? { ...candidate, status: 'placed' } : candidate)) })() }}><Bookmark size={14}/><span>{item.kind[0].toUpperCase() + item.kind.slice(1)}<small>{item.quote || item.text || item.note || 'No note text'} · {item.status === 'placed' ? 'Placed' : 'Unplaced'}</small></span></button></div>) : <p className="reader-empty-bookmarks">No imported notes or highlights.</p>}
      </nav>}
      <div className={`reader-content reader-content-${layout}`} tabIndex={0} onKeyDown={event => { if (event.target !== event.currentTarget && (event.target as HTMLElement).closest('button,input,select,textarea')) return; if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') { event.preventDefault(); turnPage(event.key === 'ArrowLeft' ? -1 : 1) } }} onClick={event => { if (layout === 'continuous' || (event.target as HTMLElement).closest('button')) return; const bounds = event.currentTarget.getBoundingClientRect(); turnPage(event.clientX < bounds.left + bounds.width / 2 ? -1 : 1) }}>
        {engineMode && selectedFile && <ReaderSurface key={`${book.id}:${selectedFile.id}`} ref={engine} bookID={book.id} file={selectedFile} legacyProgress={progress} settings={readerSettings} onSections={setEngineSections} onLocation={(locator, index) => { setEngineChapterIndex(index); setEngineProgress(locator.progress); setProgress({ chapterIndex: index, position: locator.progress }); setSavedPosition(locator.progress) }} onError={setEngineError} onLoading={setEngineLoading}/>}
        {engineMode ? (readerLoading || readerError) && <div className={`reader-engine-message ${readerError ? 'preview-error' : ''}`}>{readerError ? <><AlertCircle/>{readerError}</> : <><LoaderCircle className="spinning"/>Opening book…</>}</div> : readerLoading ? <div className="preview-message"><LoaderCircle className="spinning"/>Opening book…</div> : readerError ? <div className="preview-message preview-error"><AlertCircle/>{readerError}</div> : !active ? <div className="preview-message">No readable chapters found.</div> : <iframe key={active.href} ref={frame} title={`${book.title}: ${active.title}`} sandbox="allow-same-origin" referrerPolicy="no-referrer" src={bridge.preview ? undefined : safeHref || undefined} srcDoc={previewHtml} style={{ width: layout === 'two-page' ? 'min(1280px, 100%)' : 'min(760px, 100%)' }} onLoad={onFrameLoad}/>}
        {layout !== 'continuous' && !readerLoading && !readerError && active && <><button className="reader-page-turn reader-page-turn-prev" aria-label="Previous page" title="Previous page" onClick={() => turnPage(-1)}><ChevronLeft size={21}/></button><button className="reader-page-turn reader-page-turn-next" aria-label="Next page" title="Next page" onClick={() => turnPage(1)}><ChevronRight size={21}/></button></>}
      </div>
    </div>
    <div className="reader-progress"><span style={{ width: `${engineMode ? currentProgress * 100 : (currentChapterIndex + percent / 100) / Math.max(1, chapters.length) * 100}%` }}/></div>
  </section>
}
function escapeHTML(value: string) { return value.replace(/[&<>"']/g, char => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[char]!) ) }

function DeviceDisconnected({ ejecting, onConnect }: { ejecting: string; onConnect: () => void }) {
  return <div className="empty-device"><div className="empty-art">{ejecting ? <LoaderCircle className="spinning" size={32}/> : <><HardDrive size={35}/><span>×</span></>}</div><h2>{ejecting ? `Ejecting ${ejecting}…` : 'No reader connected'}</h2><p>{ejecting ? 'Wait while the device disconnects safely.' : 'Connect a reader to browse or manage its books.'}</p>{!ejecting && <button className="toolbar-button" onClick={onConnect}><Plus size={16}/> Add device</button>}</div>
}

function DeviceList({ deviceBooks, deviceName, layout, hasBooks, searching, loading, error, selection, setSelection, onClearSearch, onRefresh, onOpen, menu, onMenu, onMenuClose, onPreview, onRemove }: { deviceBooks: DeviceBook[]; deviceName: string; layout: 'grid' | 'list'; hasBooks: boolean; searching: boolean; loading: boolean; error: string; selection: string[]; setSelection: React.Dispatch<React.SetStateAction<string[]>>; onClearSearch: () => void; onRefresh: () => void; onOpen: (book: DeviceBook) => void; menu: MenuAnchor | null; onMenu: (key: string, button: HTMLButtonElement) => void; onMenuClose: () => void; onPreview: (bookID: string) => void; onRemove: (path: string) => void }) {
  if (!deviceBooks.length && loading) return <div className="empty-device" role="status" aria-live="polite"><div className="empty-art"><LoaderCircle className="spinning" size={32}/></div><h2>Reading {deviceName} contents…</h2><p>Looking for books on the connected device.</p></div>
  if (!deviceBooks.length && !hasBooks && error) return <div className="empty-device"><div className="empty-art"><AlertCircle size={32}/></div><h2>Couldn’t read {deviceName}</h2><p>{error}</p><button className="toolbar-button" onClick={onRefresh}><RefreshCw size={16}/> Try again</button></div>
  return <>{error && <InlineBanner className="device-load-error" action={<button className="quiet-button" onClick={onRefresh}>Try again</button>}>Couldn’t refresh {deviceName}. Showing the last loaded books.</InlineBanner>}{!deviceBooks.length ? <div className="empty-device"><div className="empty-art"><HardDrive size={35}/><span>· · ·</span></div><h2>{searching && hasBooks ? `No ${deviceName} books match these filters` : `No books found on ${deviceName}`}</h2><p>{searching && hasBooks ? 'Try another search or clear your filters.' : 'Books in its configured book folders will appear here.'}</p>{searching && hasBooks ? <button className="quiet-button" onClick={onClearSearch}>Clear filters</button> : <button className="toolbar-button" onClick={onRefresh}><RefreshCw size={16}/> Refresh device</button>}</div> : layout === 'grid' ? <div className="book-collection device-collection">{deviceBooks.map(book => <DeviceTile key={book.path} book={book} selected={selection.includes(book.path)} menu={menu?.key === `device:${book.path}` ? menu : null} onMenu={button => onMenu(`device:${book.path}`, button)} onMenuClose={onMenuClose} onPreview={() => onPreview(book.bookID)} onRemove={() => onRemove(book.path)} onToggle={() => setSelection(s => s.includes(book.path) ? s.filter(p => p !== book.path) : [...s, book.path])} onOpen={() => onOpen(book)}/>)}</div> : <div className="device-list" role="list">
    {deviceBooks.map(book => <div role="listitem" className={`device-book ${selection.includes(book.path) ? 'row-selected' : ''}`} key={book.path}>
      <button className={`device-check ${selection.includes(book.path) ? 'checked' : ''}`} onClick={() => setSelection(s => s.includes(book.path) ? s.filter(p => p !== book.path) : [...s, book.path])} aria-label={`Select ${book.title || fileTitle(book.name)}`} aria-pressed={selection.includes(book.path)}>{selection.includes(book.path) && <Check size={13}/>}</button>
      <div className="device-book-icon"><BookOpen size={17}/></div><div className="device-book-copy"><button onClick={() => onOpen(book)}>{book.title || fileTitle(book.name)}</button><span>{extensionFromPath(book.path)} · {fmtBytes(book.size)}</span></div><span className="device-book-status">On device</span><div className="book-menu-anchor" data-book-menu><button className="tile-more" aria-label={`Actions for ${book.title || fileTitle(book.name)}`} aria-haspopup="menu" aria-expanded={menu?.key === `device:${book.path}`} onClick={e => { e.stopPropagation(); onMenu(`device:${book.path}`, e.currentTarget) }}><MoreHorizontal size={18}/></button></div>
      {menu?.key === `device:${book.path}` && <ContextMenu anchor={menu} onClose={onMenuClose} items={[{ label: 'View details', icon: <BookOpen/>, onSelect: () => onOpen(book) }, { label: 'Read', icon: <BookOpen/>, onSelect: () => onPreview(book.bookID), disabled: !book.bookID || !book.canRead }, { label: 'Remove from device', icon: <Trash2/>, onSelect: () => onRemove(book.path), danger: true }]}/>}
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
    {menu && <ContextMenu anchor={menu} onClose={onMenuClose} items={[{ label: 'View details', icon: <BookOpen/>, onSelect: onOpen }, { label: 'Read', icon: <BookOpen/>, onSelect: onPreview, disabled: !book.bookID || !book.canRead }, { label: 'Remove from device', icon: <Trash2/>, onSelect: onRemove, danger: true }]}/>}
  </article>
}

function DeviceDetailsPanel({ book, deviceName, libraryBook, onClose, onPreview }: { book: DeviceBook; deviceName: string; libraryBook?: Book; onClose: () => void; onPreview: (book: Book) => void }) {
  const title = book.title || fileTitle(book.name)
  const cover = book.coverURL ? bridge.previewCover(book.coverURL) : missingCover
  const canRead = !!libraryBook && (!libraryBook.files?.length || libraryBook.files.some(file => file.canRead))
  return <aside className="details-panel device-details-panel" aria-label="Device book details"><div className="details-top"><span>ON {deviceName.toUpperCase()}</span><button className="icon-button" onClick={onClose} aria-label="Close book details"><PanelRightClose size={18}/></button></div>
    <div className="detail-cover"><img src={cover} alt={`${title} cover`} onError={e => { e.currentTarget.src = missingCover }}/></div><h2 className="detail-title">{title}</h2><p className="detail-author">{book.authors?.join(', ') || 'Unknown author'}</p>
    <div className="details-actions">{canRead && libraryBook ? <button className="toolbar-button primary-button" onClick={() => onPreview(libraryBook)}><BookOpen size={15}/> Read</button> : <p className="device-preview-unavailable">This format can’t be read in Attune yet.</p>}</div>
    <MetadataDetails metadata={book} format={extensionFromPath(book.path)} fileSize={book.size}/>
  </aside>
}

function ResizeHandle({ width, onWidthChange }: { width: number; onWidthChange: (width: number) => void }) {
  const dragging = useRef(false)
  const setFromPointer = (clientX: number) => onWidthChange(Math.max(300, Math.min(480, window.innerWidth - clientX - 12)))
  return <button className="detail-resize-handle" role="separator" aria-label="Resize book details panel" aria-orientation="vertical" aria-valuemin={300} aria-valuemax={480} aria-valuenow={width} tabIndex={0}
    onPointerDown={e => { e.preventDefault(); e.currentTarget.focus(); dragging.current = true; e.currentTarget.setPointerCapture(e.pointerId) }}
    onPointerMove={e => { if (dragging.current) setFromPointer(e.clientX) }}
    onPointerUp={e => { dragging.current = false; if (e.currentTarget.hasPointerCapture(e.pointerId)) e.currentTarget.releasePointerCapture(e.pointerId) }}
    onPointerCancel={() => { dragging.current = false }}
    onKeyDown={e => { if (e.key === 'ArrowLeft') { e.preventDefault(); onWidthChange(Math.min(480, width + 16)) } else if (e.key === 'ArrowRight') { e.preventDefault(); onWidthChange(Math.max(300, width - 16)) } }} />
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
  return <div className="empty-state"><div className="empty-art"><div className="empty-book book-a"/><div className="empty-book book-b"/><div className="empty-book book-c"/><span className="sparkle s1">✳</span><span className="sparkle s2">✦</span></div><h2>{searching ? 'No books match that search' : recent ? 'Nothing new just yet' : 'Attune your library'}</h2><p>{searching ? 'Try a different title or author.' : recent ? 'Books you add in the last 30 days will show up here.' : 'Bring your books together in one lovely little library.'}</p>{searching ? <button className="quiet-button" onClick={onClear}>Clear search</button> : !recent && <><button className="toolbar-button primary-button" onClick={onImport}><Plus size={17}/> Import books</button><span className="drop-hint">or drop supported book files anywhere in this window</span></>}</div>
}

const missingCover = `data:image/svg+xml,${encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 420 620"><rect width="420" height="620" fill="#e6e6e8"/><path d="M145 230c0-12 10-22 22-22h80c12 0 22 10 22 22v150c0 12-10 22-22 22h-80c-12 0-22-10-22-22z" fill="#c8c9cd"/><path d="M210 208v194" stroke="#eee" stroke-width="7"/><path d="M163 263h38m-38 22h32m-32 22h36m38-44h28m-28 22h28m-28 22h28" stroke="#eee" stroke-width="5" stroke-linecap="round"/></svg>')}`
function DetailsPanel({ book, editing, draft, setDraft, canSend, onClose, onEdit, onSave, onCancel, onDelete, onSend, onPreview, onOpenFile, onAttachFiles, onChangeCover, coverDisabled }: { book: Book; editing: boolean; draft: EditDraft; setDraft: React.Dispatch<React.SetStateAction<EditDraft>>; canSend: boolean; onClose: () => void; onEdit: () => void; onSave: () => void; onCancel: () => void; onDelete: () => void; onSend: () => void; onPreview: (fileID?: string) => void; onOpenFile: (fileID: string) => void; onAttachFiles: () => void; onChangeCover: () => void; coverDisabled: boolean }) {
  const update = (key: keyof EditDraft, value: string) => setDraft(current => ({ ...current, [key]: value }))
  const files = book.files ?? []
  const hasReadableFile = files.some(file => file.canRead)
  const preferredFormat = files.find(file => file.id === book.preferredFileID)?.format ?? files.find(file => file.preferred)?.format ?? files[0]?.format ?? 'EPUB'
  return <aside className="details-panel" aria-label="Book details">
    <div className="details-top"><span>BOOK DETAILS</span><button className="icon-button" onClick={onClose} aria-label="Close book details"><PanelRightClose size={18}/></button></div>
    <div className="detail-cover"><img src={bookCover(book)} alt={`${book.title} cover`} onError={e => { e.currentTarget.src = missingCover }}/></div>
    <div className="cover-edit-control"><button className="toolbar-button" onClick={onChangeCover} disabled={coverDisabled}><ImagePlus size={15}/> Change cover</button><span>JPEG or PNG · saved immediately</span></div>
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
    </div> : <><h2 className="detail-title">{book.title}</h2><p className="detail-author">{book.authors.join(', ') || 'Unknown author'}</p><div className="details-actions">{hasReadableFile && <button className="toolbar-button primary-button" onClick={() => onPreview()}><BookOpen size={15}/> Read</button>}{canSend && <button className="toolbar-button" onClick={onSend}><Send size={15}/> Send to device</button>}<button className="toolbar-button" onClick={onEdit}><Pencil size={15}/> Edit details</button><button className="toolbar-button danger-button" onClick={onDelete}><Trash2 size={15}/> Remove from library</button></div><section className="book-files" aria-label="Book files"><div className="book-files-heading"><h3>Files</h3><button className="quiet-button" disabled={coverDisabled} onClick={onAttachFiles}><Plus size={14}/> Add files</button></div>{files.length ? files.map(file => <div className="book-file-row" key={file.id}><div><strong>{file.format.toUpperCase()}</strong><span>{file.filename} · {fmtBytes(file.size)}{file.preferred ? ' · Preferred' : ''}</span></div>{file.canRead ? <button className="quiet-button" onClick={() => onPreview(file.id)}>Read</button> : <button className="quiet-button" onClick={() => onOpenFile(file.id)}>Open</button>}</div>) : <p className="book-files-empty">No files added yet.</p>}</section><MetadataDetails metadata={book} format={preferredFormat.toUpperCase()} fileSize={book.fileSize}/></>}
    <div className="detail-footer"><span>Imported book files are stored in your Attune library.</span><CircleHelp size={15}/></div>
  </aside>
}

function IntakeDialog({ request, onCancel, onImportOnly, onSend }: { request: IntakeRequest; onCancel: () => void; onImportOnly: () => void; onSend: () => void }) {
  const cancelRef = useRef<HTMLButtonElement>(null)
  useEffect(() => { cancelRef.current?.focus() }, [])
  const summary = request.names.slice(0, 4).join(', ')
  function trapFocus(e: React.KeyboardEvent<HTMLElement>) {
    if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); onCancel(); return }
    if (e.key !== 'Tab') return
    const buttons = e.currentTarget.querySelectorAll<HTMLButtonElement>('button:not([disabled])')
    if (e.shiftKey && document.activeElement === buttons[0]) { e.preventDefault(); buttons[buttons.length - 1]?.focus() }
    else if (!e.shiftKey && document.activeElement === buttons[buttons.length - 1]) { e.preventDefault(); buttons[0]?.focus() }
  }
  return <div className="confirm-backdrop" onMouseDown={e => { if (e.target === e.currentTarget) onCancel() }}>
    <section className="confirm-dialog intake-dialog" role="dialog" aria-modal="true" aria-labelledby="intake-title" aria-describedby="intake-description" onKeyDown={trapFocus}>
      <h2 id="intake-title">Import {request.names.length} {request.names.length === 1 ? 'book' : 'books'}?</h2>
      <p id="intake-description">{summary}{request.names.length > 4 ? ` and ${request.names.length - 4} more` : ''}</p>
      <div className="confirm-actions intake-actions">
        <button ref={cancelRef} className="toolbar-button" onClick={onCancel}>Cancel</button>
        <button className="toolbar-button" onClick={onImportOnly}>Import to library only</button>
        {request.device && <button className="toolbar-button primary-button" onClick={onSend}>Import and send to {request.device.name}</button>}
      </div>
    </section>
  </div>
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
      <div className="confirm-actions"><button ref={cancelRef} className="toolbar-button" onClick={onCancel}>{confirmation.cancelLabel || 'Cancel'}</button><button className={`toolbar-button ${confirmation.danger === false ? 'primary-button' : 'danger-button'} confirm-action`} onClick={onConfirm}>{confirmation.confirmLabel}</button></div>
    </section>
  </div>
}

function ImportReportDialog({ result, onClose }: { result: ImportResult; onClose: () => void }) {
  return <div className="confirm-backdrop" onMouseDown={event => { if (event.target === event.currentTarget) onClose() }}>
    <section className="confirm-dialog import-report-dialog" role="dialog" aria-modal="true" aria-labelledby="import-report-title">
      <h2 id="import-report-title">Import report</h2>
      <p>{result.imported} added · {result.duplicates} already present{result.skipped ? ` · ${result.skipped} skipped` : ''}. {result.errors.length} file {result.errors.length === 1 ? 'issue' : 'issues'} need attention.</p>
      <ul className="import-report-errors">{result.errors.map((error, index) => <li key={`${index}-${error}`}>{error}</li>)}</ul>
      <div className="confirm-actions"><button className="toolbar-button primary-button" autoFocus onClick={onClose}>Done</button></div>
    </section>
  </div>
}

function DevicePreferencesDialog({ draft, onChange, onClose, onSave, onReset, disabled }: { draft: DevicePreferencesDraft; onChange: React.Dispatch<React.SetStateAction<DevicePreferencesDraft | null>>; onClose: () => void; onSave: () => void; onReset: () => void; disabled: boolean }) {
  const folders = draft.foldersText.split('\n').map(folder => folder.trim()).filter(Boolean)
  const toggleFormat = (format: string) => onChange(current => {
    if (!current) return current
    const formats = current.formats.includes(format) ? current.formats.filter(value => value !== format) : [...current.formats, format]
    return { ...current, formats }
  })
  return <div className="confirm-backdrop" onMouseDown={event => { if (event.target === event.currentTarget) onClose() }}>
    <section className="confirm-dialog device-preferences-dialog" role="dialog" aria-modal="true" aria-labelledby="device-preferences-title">
      <h2 id="device-preferences-title">Transfer settings</h2>
      <p>{draft.deviceName} · Settings are saved for this device.</p>
      {draft.requiresExplicitSelection && <div className="device-profile-note">This device has no recognized profile. Choose where Attune should place books and which formats it can open before sending.</div>}
      <label className="device-folder-field">Book folders <span>Relative to the device root. Put preferred folders first.</span><textarea rows={3} value={draft.foldersText} placeholder={'Books\nDocuments'} onChange={event => onChange(current => current ? { ...current, foldersText: event.target.value } : current)}/></label>
      <fieldset className="device-format-field"><legend>Formats this device can open</legend><div className="device-format-options">{deviceFormatOptions.map(format => <label key={format}><input type="checkbox" checked={draft.formats.includes(format)} onChange={() => toggleFormat(format)}/><span>{format.toUpperCase()}</span></label>)}</div></fieldset>
      <div className="confirm-actions device-preference-actions"><button className="quiet-button device-reset" onClick={onReset} disabled={disabled || !draft.customized}>Restore profile defaults</button><button className="toolbar-button" onClick={onClose}>Cancel</button><button className="toolbar-button primary-button" onClick={onSave} disabled={disabled || !folders.length || !draft.formats.length}>Save settings</button></div>
    </section>
  </div>
}

function Settings({ updates, onUpdates, textSize, setTextSize, state, onOpenFolder, onChangeLocation, onImportCalibre, changingLibrary, controlsDisabled }: { updates: UpdateState; onUpdates: () => void; textSize: TextSize; setTextSize: (size: TextSize) => void; state: AppState | null; onOpenFolder: () => void; onChangeLocation: () => void; onImportCalibre: () => void; changingLibrary: boolean; controlsDisabled: boolean }) {
  const location = state?.libraryPath || 'Unavailable'
  return <section className="settings-page"><div className="settings-heading"><div className="settings-icon"><Settings2 size={22}/></div><div><p>A few details about your library.</p></div></div><div className="settings-card text-size-card"><div><strong>Text size</strong><span>Adjust the size of labels and controls.</span></div><div className="text-size-options" role="group" aria-label="Text size">{(['small','medium','large'] as TextSize[]).map(size => <button key={size} className={textSize === size ? 'selected' : ''} aria-pressed={textSize === size} onClick={() => setTextSize(size)}>{size === 'medium' ? 'Default' : size[0].toUpperCase() + size.slice(1)}</button>)}</div></div><div className="settings-card"><div><strong>Library location</strong><span title={location}>{location}</span></div><div className="settings-actions"><button className="toolbar-button" onClick={onOpenFolder} disabled={controlsDisabled}><FolderOpen size={16}/> Open folder</button><button className="toolbar-button" onClick={onChangeLocation} disabled={controlsDisabled} aria-busy={changingLibrary}>{changingLibrary ? <><LoaderCircle size={16} className="spinning"/> Copying library…</> : <>Change…</>}</button></div></div><div className="settings-card calibre-import-card"><div><strong>Import Calibre library</strong><span>Copies books, metadata and covers into Attune. Your Calibre library stays unchanged.</span></div><button className="toolbar-button" onClick={onImportCalibre} disabled={controlsDisabled}>Choose library…</button></div><div className="settings-card"><div><strong>EPUB converter</strong><span>{state?.converterReady ? `Ready · ${state.converterVersion || 'installed'}` : 'Not available yet'}</span></div><span className={`converter-status ${state?.converterReady ? 'ready' : ''}`}>{state?.converterReady ? 'Ready' : 'Unavailable'}</span></div><UpdateSettingsCard state={updates} onOpen={onUpdates}/><div className="settings-note"><CircleHelp size={16}/><span>Attune creates an Attune Library folder in the location you choose. Your books and metadata are copied; the original folder is kept.</span></div></section>
}

function stageName(stage: string) { return ({ queued: 'Waiting in queue', converting: 'Preparing a compatible format', transferring: 'Copying to device', done: 'Saved to device', cancelled: 'Transfer cancelled', error: 'Check your device and try again' } as Record<string, string>)[stage] ?? 'Preparing transfer' }
function message(e: unknown) { return e instanceof Error ? e.message : String(e || 'Something went wrong. Try again.') }
