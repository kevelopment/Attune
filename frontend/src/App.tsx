import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import {
  BookOpen, Clock3, HardDrive, Settings2, Search, Plus, Grid2X2, List, ArrowDownUp, ArrowLeft, ArrowRight,
  MoreHorizontal, X, Pencil, Trash2, Send, RefreshCw, Power, Check, AlertCircle,
  CircleHelp, FolderOpen, Upload, Download, LoaderCircle, PanelRightClose, ImagePlus,
} from 'lucide-react'
import { bridge, progressFromEvent } from './bridge'
import type { AppState, Book, BookMetadataEdit, BookPreview, Device, DeviceBook, Progress, ImportProgress, ImportResult } from './types'
import { useTextSize, useBookSize, type TextSize } from './appearance'
import BookSizeControl from './GallerySize'
import ActivityCenter, { useActivityLog } from './ActivityCenter'
import type { ActivityEntry } from './ActivityCenter'
import { InlineBanner, useNotifications } from './Notifications'
import Updates, { useUpdater, UpdateSettingsCard } from './Updates'
import type { UpdateState } from './types'
import DropdownSelect from './DropdownSelect'

type Page = 'library' | 'recent' | 'device' | 'updates' | 'settings'
type Sort = 'recent' | 'title' | 'author'
type Confirmation = { title: string; description: string; confirmLabel: string; cancelLabel?: string; danger?: boolean; action: () => void }
type EditDraft = { title: string; authors: string; publisher: string; language: string; published: string; subjects: string; series: string; seriesIndex: string; description: string }
type MenuAnchor = { key: string; left: number; top: number }
type IntakeRequest = { paths: string[]; names: string[]; device: { id: string; name: string } | null }
type PasteMenu = { left: number; top: number } | null
const fmtBytes = (n: number) => n < 1_000 ? `${n} B` : n < 1_000_000 ? `${(n / 1_000).toFixed(0)} KB` : n < 1_000_000_000 ? `${(n / 1_000_000).toFixed(1)} MB` : `${(n / 1_000_000_000).toFixed(1)} GB`
function bookCover(book: Book) {
  const url = bridge.previewCover(book.coverURL || `/covers/${book.id}`)
  return bridge.preview ? url : `${url}${url.includes('?') ? '&' : '?'}revision=${book.revision}`
}
const recentWindowMs = 30 * 86400000
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
  const [ejectingDevice, setEjectingDevice] = useState<Device | null>(null)
  const [confirmation, setConfirmation] = useState<Confirmation | null>(null)
  const [previewTarget, setPreviewTarget] = useState<Book | null>(null)
  const [bookPreview, setBookPreview] = useState<BookPreview | null>(null)
  const [previewLoading, setPreviewLoading] = useState(false)
  const [previewError, setPreviewError] = useState('')
  const [previewChapter, setPreviewChapter] = useState(0)
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
    setNavigation(current => {
      if (current.pages[current.index] === nextPage) return current
      const pages = [...current.pages.slice(0, current.index + 1), nextPage]
      return { pages, index: pages.length - 1 }
    })
  }

  const refresh = useCallback(async (force = false) => {
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
  useEffect(() => { void refresh() }, [refresh])
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
          if (next.stage === 'done' && next.total > 0 && next.current >= next.total) notify({ text: `${next.title || 'Books'} sent to Kindle`, kind: 'success' })
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
  const device = state?.devices?.find(item => !hiddenEjectingDevices.current.has(item.id) && !ejectingDeviceIDs.current.has(item.id))
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
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'v' && !typing) { e.preventDefault(); pasteFilesRef.current(); return }
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'f') { e.preventDefault(); searchRef.current?.focus() }
      if (e.key === 'Escape') {
        if (activeMenu || pasteMenu) { setActiveMenu(null); setPasteMenu(null); e.preventDefault(); return }
        if (intake) setIntake(null)
        else if (confirmation) setConfirmation(null)
        else if (previewTarget) { setPreviewTarget(null); setBookPreview(null) }
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
      if (!result || (!result.imported && !result.duplicates && !result.errors?.length)) {
        removeActivity(activityID)
        return result ?? null
      }
      const errors = result.errors ?? []
      const imported = result.imported ?? 0
      const duplicates = result.duplicates ?? 0
      updateActivity(activityID, { status: errors.length ? 'error' : 'success', current: result.imported + result.duplicates + errors.length, total: total || result.imported + result.duplicates + errors.length, detail: errors[0] || `${imported} imported · ${duplicates} duplicates` })
      if (errors.length) notify({ text: `${imported} imported · ${duplicates} duplicates · ${errors.length} failed: ${errors[0]}`, kind: 'error' })
      else notify({ text: `${imported} imported · ${duplicates} duplicates`, kind: 'success' })
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
    if (target === 'device') setIntake({ paths, names, device: device ? { id: device.id, name: device.name } : null })
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
      if (!paths.length) { notify({ text: 'No EPUB files were found on the clipboard.', kind: 'error' }); return }
      const names = paths.map(path => path.split(/[\\/]/).pop() ?? path)
      setIntake({ paths, names, device: device ? { id: device.id, name: device.name } : null })
    } catch (e) { notify({ text: message(e), kind: 'error' }) }
    finally { clipboardReadInFlight.current = false }
  }
  pasteFilesRef.current = () => { void requestPasteFiles() }
  async function completeIntake(request: IntakeRequest, sendToDevice: boolean) {
    if (importGuard.current || busy || transferActive || changingLibrary) return
    const result = await performImport(() => bridge.importPaths(request.paths), request.paths.length, request.names[0] ?? 'Imported books')
    if (!sendToDevice || !result || !result.bookIDs?.length) return
    const target = request.device
    if (!target) { notify({ text: 'No Kindle was connected when you chose to send these books.', kind: 'error' }); return }
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
  async function addDevice() {
    if (ejectingDeviceIDs.current.size) return
    await run(() => bridge.selectDevice(), async () => { await refresh(); navigateTo('device') })
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
    setConfirmation({ title: 'Remove from library?', description: `“${book.title}” will be removed from your library. Any copy already on a Kindle will stay there.`, confirmLabel: 'Remove from library', action: () => { void run(() => bridge.deleteBook(book.id), async () => { setSelected(s => s.filter(id => id !== book.id)); setFocusedBook(null); await refresh(); notify({ text: 'Book removed from your library', kind: 'success' }) }) } })
  }
  async function deleteSelected() {
    const targets = (state?.books ?? []).filter(b => selected.includes(b.id))
    if (!targets.length) return
    setConfirmation({ title: `Remove ${targets.length} ${targets.length === 1 ? 'book' : 'books'} from library?`, description: 'Any copies already on a Kindle will stay there.', confirmLabel: 'Remove from library', action: () => { void run(async () => { for (const book of targets) await bridge.deleteBook(book.id) }, async () => { setSelected([]); await refresh(); notify({ text: `${targets.length} ${targets.length === 1 ? 'book' : 'books'} removed from your library`, kind: 'success' }) }) } })
  }
  async function send(ids: string[], target: { id: string; name: string } | null = device ? { id: device.id, name: device.name } : null) {
    if (!ids.length) return
    if (intakeFlowGuard.current || transferActive) { notify({ text: 'Wait for the current operation to finish before starting another.', kind: 'status' }); return }
    if (!target) { notify({ text: 'Connect a Kindle first, then choose it in Devices.', kind: 'error' }); return }
    transferGuard.current = true
    const title = ids.length === 1 ? (state?.books ?? []).find(b => b.id === ids[0])?.title ?? 'Book' : `${ids.length} books`
    transferActivityId.current = begin('transfer', `Send ${title} to ${target.name}`)
    updateActivity(transferActivityId.current, { status: 'running', current: 0, total: ids.length, detail: 'Preparing transfer' })
    setProgress({ id: 'starting', bookID: '', title, stage: 'queued', message: 'Preparing transfer', current: 0, total: ids.length })
    const ok = await run(() => bridge.sendBooks(target.id, ids), () => setSelected([]))
    if (ok === null) {
      transferGuard.current = false
      if (transferActivityId.current) updateActivity(transferActivityId.current, { status: 'error', detail: 'Could not start transfer. Check the Kindle connection and try again.' })
      setProgress(p => p ? { ...p, stage: 'error', message: 'Could not start transfer. Check the Kindle connection and try again.' } : null)
    }
  }
  function eject() {
    if (!device) return
    const target = device
    setConfirmation({ title: 'Eject Kindle?', description: 'Finish writing to the device and disconnect it safely.', confirmLabel: 'Eject Kindle', action: () => {
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
          notify({ text: 'Kindle ejected safely', kind: 'success' })
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
    setConfirmation({ title: `Remove ${paths.length} ${single ? 'book' : 'books'} from Kindle?`, description: 'Your library copies will stay on this computer.', confirmLabel: 'Remove from Kindle', action: () => { void run(() => bridge.removeDeviceBooks(device.id, paths), async () => { setDeviceSelection(s => s.filter(path => !paths.includes(path))); setDeviceBooks(await bridge.getDeviceBooks(device.id)); notify({ text: `${single ? 'Book' : 'Books'} removed from Kindle`, kind: 'success' }) }) } })
  }
  function removeFromDevice() { confirmRemoveFromDevice([...deviceSelection]) }
  function toggle(id: string, e?: React.MouseEvent) { setSelected(s => s.includes(id) ? s.filter(x => x !== id) : e?.shiftKey ? [...s, id] : [...s, id]) }

  const hasFilters = !!(authorFilter || genreFilter || languageFilter)
  const openDeviceDetails = (book: DeviceBook) => { setFocusedBook(null); setFocusedDeviceBook(book) }
  const renderBook = (book: Book, itemLayout: 'grid' | 'list') => (
    <BookTile key={book.id} book={book} layout={itemLayout} selected={selected.includes(book.id)} menu={activeMenu?.key === `book:${book.id}` ? activeMenu : null} onMenu={button => toggleMenu(`book:${book.id}`, button)} onMenuClose={() => setActiveMenu(null)} canSend={!!device && !transferActive} sendLabel={device ? `Send to ${device.name}` : 'Connect a Kindle to send'} onPreview={() => { setActiveMenu(null); void openPreview(book) }} onEdit={() => { setActiveMenu(null); void startEdit(book) }} onSend={() => { setActiveMenu(null); void send([book.id]) }} onDelete={() => { setActiveMenu(null); void deleteLocal(book) }} onToggle={e => toggle(book.id, e)} onOpen={() => { setFocusedDeviceBook(null); setFocusedBook(book); setEditing(false) }}/>
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
  const detailsVisible = page !== 'settings' && page !== 'updates'
  const detailsOpen = detailsVisible && !!(focusedBook || focusedDeviceBook)
  const resizeHandle = <ResizeHandle width={detailWidth} onWidthChange={setDetailWidth}/>
  return <div className={`app-shell ${bridge.preview ? 'preview-mode' : ''} ${detailsOpen ? 'details-open' : ''}`} style={detailsOpen ? { gridTemplateColumns: `64px minmax(320px, 1fr) ${detailWidth}px` } : undefined}>
    <aside className="sidebar" aria-label="Main navigation">
      <nav className="rail-nav" aria-label="Library sections">
        <button className={`rail-item ${page === 'library' ? 'active' : ''}`} aria-label="All books" aria-current={page === 'library' ? 'page' : undefined} onClick={() => navigateTo('library')}><BookOpen/></button>
        <button className={`rail-item ${page === 'recent' ? 'active' : ''}`} aria-label="Recently added" aria-current={page === 'recent' ? 'page' : undefined} onClick={() => navigateTo('recent')}><Clock3/></button>
        <button className={`rail-item ${page === 'device' ? 'active' : ''}`} data-drop-target={device ? 'device' : 'library'} aria-label="Reader" aria-current={page === 'device' ? 'page' : undefined} onClick={() => navigateTo('device')}>{device ? <span className="device-glyph"><KindleIcon/><i/></span> : <HardDrive/>}</button>
      </nav>
      <div className="rail-spacer"/>
      {(updater.state.available || updater.state.downloaded) && <button className={`rail-item rail-updates ${page === 'updates' ? 'active' : ''}`} aria-label="Update available" aria-busy={updater.state.status === 'downloading'} aria-current={page === 'updates' ? 'page' : undefined} onClick={() => { navigateTo('updates'); if (!updater.state.downloaded && updater.state.status !== 'downloading') void downloadUpdate() }}><Download/></button>}
      <button className={`rail-item rail-settings ${page === 'settings' ? 'active' : ''}`} aria-label="Settings" aria-current={page === 'settings' ? 'page' : undefined} onClick={() => navigateTo('settings')}><Settings2/></button>
    </aside>

    <header className="topbar" data-drop-target={isDevice ? 'device' : 'library'}>
      <div className="topbar-leading">
        <div className="topbar-navigation" aria-label="Navigation history">
          <button className="history-button" aria-label="Back" title="Back" disabled={navigation.index === 0} onClick={() => setNavigation(current => ({ ...current, index: Math.max(0, current.index - 1) }))}><ArrowLeft size={17}/></button>
          <button className="history-button" aria-label="Forward" title="Forward" disabled={navigation.index >= navigation.pages.length - 1} onClick={() => setNavigation(current => ({ ...current, index: Math.min(current.pages.length - 1, current.index + 1) }))}><ArrowRight size={17}/></button>
        </div>
        <h1 className="global-page-title">{page === 'library' ? `All books (${books.length})` : page === 'recent' ? 'Recently added' : page === 'device' ? 'Reader' : page === 'settings' ? 'Settings' : 'Updates'}</h1>
      </div>
      <div className="topbar-center">
        <ActivityCenter entries={entries} onCancelTransfer={() => void run(() => bridge.cancelTransfer())}/>
        {page !== 'settings' && page !== 'updates' && <label className="search-box"><Search size={17}/><input ref={searchRef} placeholder="Search books" aria-label="Search books" value={query} onChange={e => setQuery(e.target.value)}/><kbd>{/Mac|iPhone|iPad/.test(navigator.platform) ? '⌘ F' : 'Ctrl+F'}</kbd>{query && <button className="clear-search" onClick={() => setQuery('')} aria-label="Clear search"><X size={14}/></button>}</label>}
      </div>
      <div className="top-actions">
        {page === 'device' && device && <button className="toolbar-button" title="Refresh Kindle contents" onClick={() => deviceRefreshRef.current()}><RefreshCw size={16}/><span>Refresh</span></button>}
        {page !== 'settings' && page !== 'updates' && <button className="toolbar-button primary-button" onClick={() => void beginImport()} disabled={busy || transferActive}><Plus size={17}/><span>Import books</span></button>}
      </div>
    </header>

    <main className="main-panel" style={{ '--book-scale': bookSize / 100 } as React.CSSProperties} data-drop-target={isDevice ? 'device' : 'library'} onContextMenu={e => { if (page !== 'library' && page !== 'recent' && page !== 'device') return; const target = e.target as HTMLElement; if (target.closest('button,input,textarea,select,a,[contenteditable=true],.book-tile,.book-row,.device-book,.book-context-menu,.details-panel,.filter-popover,.activity-center')) return; e.preventDefault(); setActiveMenu(null); setPasteMenu({ left: Math.min(e.clientX, window.innerWidth - 220), top: Math.min(e.clientY, window.innerHeight - 56) }) }}>
      {page === 'settings' ? <Settings updates={updater.state} onUpdates={() => { navigateTo('updates'); void updater.check() }} textSize={textSize} setTextSize={setTextSize} state={state} onOpenFolder={() => void run(() => bridge.openLibraryFolder())} onChangeLocation={() => void changeLibraryLocation()} changingLibrary={changingLibrary} controlsDisabled={busy || importGuard.current || transferActive}/> : page === 'updates' ? <Updates state={updater.state} checked={updater.checked} onCheck={() => void updater.check()} onDownload={() => void downloadUpdate()} onRestart={() => void updater.restart()} restartDisabled={restartDisabled}/> : <>
        {isDevice && device ? <section className="device-overview">
          <div className="device-hero-icon"><HardDrive size={25}/></div><div className="device-copy"><div className="eyebrow">CONNECTED DEVICE</div><h1>{device.name}</h1><div className="device-path">{device.root}</div></div>
          <div className="storage"><div className="storage-line"><span>{fmtBytes(device.freeBytes)} available</span><span>of {fmtBytes(device.totalBytes)}</span></div><div className="storage-track"><span style={{ width: `${Math.max(2, 100 - device.freeBytes / Math.max(device.totalBytes, 1) * 100)}%` }}/></div></div>
          <button className="toolbar-button eject-button" title="Eject Kindle" aria-label="Eject Kindle" onClick={eject}><Power size={16}/><span>Eject</span></button>
        </section> : null}
        <section className="content-heading">
          <div className="library-toolbar">
            {isDevice && <span className="book-count">{`${visibleDeviceBooks.length} ${visibleDeviceBooks.length === 1 ? 'book' : 'books'}`}</span>}
            {page === 'recent' && <span className="recent-window-caption">Last 30 days</span>}
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
              {selected.length > 0 && device && <button className="toolbar-button compact-action" title={`Send ${selected.length} selected books to Kindle`} aria-label={`Send ${selected.length} selected books to Kindle`} onClick={() => void send(selected)} disabled={transferActive}><Send size={16}/><span>Send to Kindle <em>{selected.length}</em></span></button>}
            </>}
          </div>
          {(authorFilter || genreFilter || languageFilter) && <div className="applied-filter-tags" aria-label="Applied filters">{authorFilter && <FilterTag label="Author" value={authorFilter === unknownFilter ? 'Unknown' : authorFilter} onRemove={() => setAuthorFilter('')}/ >}{genreFilter && <FilterTag label="Genre" value={genreFilter === unknownFilter ? 'Unknown' : genreFilter} onRemove={() => setGenreFilter('')}/ >}{languageFilter && <FilterTag label="Language" value={languageFilter === unknownFilter ? 'Unknown' : languageLabel(languageFilter)} onRemove={() => setLanguageFilter('')}/ >}<button className="clear-filters" onClick={() => { setAuthorFilter(''); setGenreFilter(''); setLanguageFilter('') }}>Clear all</button></div>}
        </section>
        <div className={`book-area ${dragging ? 'dragging' : ''}`} data-drop-target={isDevice ? 'device' : 'library'}>
          {dragging && <div className="drop-overlay"><div><Upload size={27}/><b>Drop EPUBs to add them</b><span>Your originals stay untouched</span></div></div>}
          {isDevice ? device ? <DeviceList deviceBooks={visibleDeviceBooks} layout={layout} hasBooks={deviceBooks.length > 0} searching={!!query.trim() || hasFilters} loading={deviceBooksLoading} error={deviceBooksError} selection={deviceSelection} setSelection={setDeviceSelection} onClearSearch={() => { setQuery(''); setAuthorFilter(''); setGenreFilter(''); setLanguageFilter('') }} onRefresh={() => deviceRefreshRef.current()} onOpen={openDeviceDetails} menu={activeMenu} onMenu={(key, button) => toggleMenu(key, button)} onMenuClose={() => setActiveMenu(null)} onPreview={previewDeviceBook} onRemove={path => { setActiveMenu(null); confirmRemoveFromDevice([path]) }}/> : <DeviceDisconnected ejecting={!!ejectingDevice} onConnect={() => void addDevice()}/> : books.length ? page === 'recent' ? <div className="recent-timeline">
            {recentGroups.map(group => <section className="recent-day" key={group.label}><h2 className="recent-day-heading">{group.label}</h2>
              {group.batches.map(batch => { const time = formatAddedTime(batch.timestamp); return <section className="recent-batch" key={batch.id}>
                <div className="recent-batch-heading"><time dateTime={time?.dateTime} title={time?.title}>{time?.time}</time><span>{batch.books.length < batch.totalBooks ? `${batch.books.length} of ${batch.totalBooks} books shown` : `${batch.totalBooks} ${batch.totalBooks === 1 ? 'book' : 'books'} added`}</span></div>
                <div className="book-collection list-layout recent-batch-books">
                  {batch.books.map(book => renderBook(book, 'list'))}
                </div>
              </section> })}
            </section>)}
          </div> : <div className={`book-collection ${layout === 'list' ? 'list-layout' : ''}`}>
            {books.map(book => renderBook(book, layout))}
          </div> : <EmptyState recent={page === 'recent'} searching={!!query || hasFilters} onImport={() => void beginImport()} onClear={() => { setQuery(''); setAuthorFilter(''); setGenreFilter(''); setLanguageFilter('') }}/ >}
        </div>
      </>}
    </main>
    {detailsVisible && focusedBook && <div className="details-column">{resizeHandle}<DetailsPanel book={focusedBook} editing={editing} draft={editDraft} setDraft={setEditDraft} onClose={() => { setFocusedBook(null); setEditing(false) }} onEdit={() => void startEdit(focusedBook)} onSave={() => void saveEdit()} onCancel={() => setEditing(false)} onDelete={() => void deleteLocal(focusedBook)} onSend={() => void send([focusedBook.id])} onPreview={() => void openPreview(focusedBook)} onChangeCover={() => void changeCover(focusedBook)} coverDisabled={busy || transferActive || changingLibrary}/></div>}
    {bridge.preview && <input hidden type="file" ref={coverInputRef} accept="image/jpeg,image/png,.jpg,.jpeg,.png" aria-label="Choose cover image" onChange={e => { const file = e.currentTarget.files?.[0]; const book = coverTarget.current; e.currentTarget.value = ''; coverTarget.current = null; if (file && book) void changeCover(book, file) }}/ >}
    {detailsVisible && focusedDeviceBook && <div className="details-column">{resizeHandle}<DeviceDetailsPanel book={focusedDeviceBook} libraryBook={(state?.books ?? []).find(book => book.id === focusedDeviceBook.bookID)} onClose={() => setFocusedDeviceBook(null)} onPreview={book => void openPreview(book)}/></div>}
    {previewTarget && <BookPreviewDialog book={bookPreview?.book ?? previewTarget} preview={bookPreview} loading={previewLoading} error={previewError} chapterIndex={previewChapter} setChapterIndex={setPreviewChapter} onClose={() => { setPreviewTarget(null); setBookPreview(null) }}/>}
    {pasteMenu && <div className="book-context-menu paste-context-menu" style={{ left: pasteMenu.left, top: pasteMenu.top }} role="menu" onContextMenu={e => e.preventDefault()}><button role="menuitem" onClick={() => { setPasteMenu(null); pasteFilesRef.current() }}><span className="menu-item-icon"><Upload size={16}/></span>Paste books <kbd>{/Mac|iPhone|iPad/.test(navigator.platform) ? '⌘V' : 'Ctrl+V'}</kbd></button></div>}
    {intake && <IntakeDialog request={intake} onCancel={() => setIntake(null)} onImportOnly={() => void confirmIntake('library')} onSend={() => void confirmIntake('send')}/>}
    {confirmation && <ConfirmDialog confirmation={confirmation} onCancel={() => setConfirmation(null)} onConfirm={() => { const action = confirmation.action; setConfirmation(null); action() }}/>}
  </div>
}

function BookTile({ book, layout, selected, menu, onMenu, onMenuClose, canSend, sendLabel, onPreview, onEdit, onSend, onDelete, onToggle, onOpen }: { book: Book; layout: 'grid' | 'list'; selected: boolean; menu: MenuAnchor | null; onMenu: (button: HTMLButtonElement) => void; onMenuClose: () => void; canSend: boolean; sendLabel: string; onPreview: () => void; onEdit: () => void; onSend: () => void; onDelete: () => void; onToggle: (e: React.MouseEvent) => void; onOpen: () => void }) {
  const cover = bookCover(book)
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

function DeviceDisconnected({ ejecting, onConnect }: { ejecting: boolean; onConnect: () => void }) {
  return <div className="empty-device"><div className="empty-art">{ejecting ? <LoaderCircle className="spinning" size={32}/> : <><HardDrive size={35}/><span>×</span></>}</div><h2>{ejecting ? 'Ejecting Kindle…' : 'No reader connected'}</h2><p>{ejecting ? 'Wait while the Kindle disconnects safely.' : 'Connect a reader to browse or manage its books.'}</p>{!ejecting && <button className="toolbar-button" onClick={onConnect}><Plus size={16}/> Add device</button>}</div>
}

function DeviceList({ deviceBooks, layout, hasBooks, searching, loading, error, selection, setSelection, onClearSearch, onRefresh, onOpen, menu, onMenu, onMenuClose, onPreview, onRemove }: { deviceBooks: DeviceBook[]; layout: 'grid' | 'list'; hasBooks: boolean; searching: boolean; loading: boolean; error: string; selection: string[]; setSelection: React.Dispatch<React.SetStateAction<string[]>>; onClearSearch: () => void; onRefresh: () => void; onOpen: (book: DeviceBook) => void; menu: MenuAnchor | null; onMenu: (key: string, button: HTMLButtonElement) => void; onMenuClose: () => void; onPreview: (bookID: string) => void; onRemove: (path: string) => void }) {
  if (!deviceBooks.length && loading) return <div className="empty-device" role="status" aria-live="polite"><div className="empty-art"><LoaderCircle className="spinning" size={32}/></div><h2>Reading Kindle contents…</h2><p>Looking for books on the connected device.</p></div>
  if (!deviceBooks.length && !hasBooks && error) return <div className="empty-device"><div className="empty-art"><AlertCircle size={32}/></div><h2>Couldn’t read Kindle contents</h2><p>{error}</p><button className="toolbar-button" onClick={onRefresh}><RefreshCw size={16}/> Try again</button></div>
  return <>{error && <InlineBanner className="device-load-error" action={<button className="quiet-button" onClick={onRefresh}>Try again</button>}>Couldn’t refresh Kindle contents. Showing the last loaded books.</InlineBanner>}{!deviceBooks.length ? <div className="empty-device"><div className="empty-art"><HardDrive size={35}/><span>· · ·</span></div><h2>{searching && hasBooks ? 'No Kindle books match these filters' : 'No books found on this Kindle'}</h2><p>{searching && hasBooks ? 'Try another search or clear your filters.' : 'Books in the device’s documents folder will appear here.'}</p>{searching && hasBooks ? <button className="quiet-button" onClick={onClearSearch}>Clear filters</button> : <button className="toolbar-button" onClick={onRefresh}><RefreshCw size={16}/> Refresh device</button>}</div> : layout === 'grid' ? <div className="book-collection device-collection">{deviceBooks.map(book => <DeviceTile key={book.path} book={book} selected={selection.includes(book.path)} menu={menu?.key === `device:${book.path}` ? menu : null} onMenu={button => onMenu(`device:${book.path}`, button)} onMenuClose={onMenuClose} onPreview={() => onPreview(book.bookID)} onRemove={() => onRemove(book.path)} onToggle={() => setSelection(s => s.includes(book.path) ? s.filter(p => p !== book.path) : [...s, book.path])} onOpen={() => onOpen(book)}/>)}</div> : <div className="device-list" role="list">
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
  return <div className="empty-state"><div className="empty-art"><div className="empty-book book-a"/><div className="empty-book book-b"/><div className="empty-book book-c"/><span className="sparkle s1">✳</span><span className="sparkle s2">✦</span></div><h2>{searching ? 'No books match that search' : recent ? 'Nothing new just yet' : 'Attune your library'}</h2><p>{searching ? 'Try a different title or author.' : recent ? 'Books you add in the last 30 days will show up here.' : 'Bring your EPUBs together in one lovely little library.'}</p>{searching ? <button className="quiet-button" onClick={onClear}>Clear search</button> : !recent && <><button className="toolbar-button primary-button" onClick={onImport}><Plus size={17}/> Import EPUBs</button><span className="drop-hint">or drop EPUB files anywhere in this window</span></>}</div>
}

const missingCover = `data:image/svg+xml,${encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 420 620"><rect width="420" height="620" fill="#e6e6e8"/><path d="M145 230c0-12 10-22 22-22h80c12 0 22 10 22 22v150c0 12-10 22-22 22h-80c-12 0-22-10-22-22z" fill="#c8c9cd"/><path d="M210 208v194" stroke="#eee" stroke-width="7"/><path d="M163 263h38m-38 22h32m-32 22h36m38-44h28m-28 22h28m-28 22h28" stroke="#eee" stroke-width="5" stroke-linecap="round"/></svg>')}`
function DetailsPanel({ book, editing, draft, setDraft, onClose, onEdit, onSave, onCancel, onDelete, onSend, onPreview, onChangeCover, coverDisabled }: { book: Book; editing: boolean; draft: EditDraft; setDraft: React.Dispatch<React.SetStateAction<EditDraft>>; onClose: () => void; onEdit: () => void; onSave: () => void; onCancel: () => void; onDelete: () => void; onSend: () => void; onPreview: () => void; onChangeCover: () => void; coverDisabled: boolean }) {
  const update = (key: keyof EditDraft, value: string) => setDraft(current => ({ ...current, [key]: value }))
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
    </div> : <><h2 className="detail-title">{book.title}</h2><p className="detail-author">{book.authors.join(', ') || 'Unknown author'}</p><div className="details-actions"><button className="toolbar-button primary-button" onClick={onPreview}><BookOpen size={15}/> Read preview</button><button className="toolbar-button" onClick={onSend}><Send size={15}/> Send to Kindle</button><button className="toolbar-button" onClick={onEdit}><Pencil size={15}/> Edit details</button><button className="toolbar-button danger-button" onClick={onDelete}><Trash2 size={15}/> Remove from library</button></div><MetadataDetails metadata={book} format="EPUB" fileSize={book.fileSize}/></>}
    <div className="detail-footer"><span>Original EPUB is safely stored on this computer.</span><CircleHelp size={15}/></div>
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
function Settings({ updates, onUpdates, textSize, setTextSize, state, onOpenFolder, onChangeLocation, changingLibrary, controlsDisabled }: { updates: UpdateState; onUpdates: () => void; textSize: TextSize; setTextSize: (size: TextSize) => void; state: AppState | null; onOpenFolder: () => void; onChangeLocation: () => void; changingLibrary: boolean; controlsDisabled: boolean }) {
  const location = state?.libraryPath || 'Unavailable'
  return <section className="settings-page"><div className="settings-heading"><div className="settings-icon"><Settings2 size={22}/></div><div><p>A few details about your library.</p></div></div><div className="settings-card text-size-card"><div><strong>Text size</strong><span>Adjust the size of labels and controls.</span></div><div className="text-size-options" role="group" aria-label="Text size">{(['small','medium','large'] as TextSize[]).map(size => <button key={size} className={textSize === size ? 'selected' : ''} aria-pressed={textSize === size} onClick={() => setTextSize(size)}>{size === 'medium' ? 'Default' : size[0].toUpperCase() + size.slice(1)}</button>)}</div></div><div className="settings-card"><div><strong>Library location</strong><span title={location}>{location}</span></div><div className="settings-actions"><button className="toolbar-button" onClick={onOpenFolder} disabled={controlsDisabled}><FolderOpen size={16}/> Open folder</button><button className="toolbar-button" onClick={onChangeLocation} disabled={controlsDisabled} aria-busy={changingLibrary}>{changingLibrary ? <><LoaderCircle size={16} className="spinning"/> Copying library…</> : <>Change…</>}</button></div></div><div className="settings-card"><div><strong>EPUB converter</strong><span>{state?.converterReady ? `Ready · ${state.converterVersion || 'installed'}` : 'Not available yet'}</span></div><span className={`converter-status ${state?.converterReady ? 'ready' : ''}`}>{state?.converterReady ? 'Ready' : 'Unavailable'}</span></div><UpdateSettingsCard state={updates} onOpen={onUpdates}/><div className="settings-note"><CircleHelp size={16}/><span>Attune creates an Attune Library folder in the location you choose. Your books and metadata are copied; the original folder is kept.</span></div></section>
}

function stageName(stage: string) { return ({ queued: 'Waiting in queue', converting: 'Converting for Kindle', transferring: 'Copying to device', done: 'Saved to Kindle', cancelled: 'Transfer cancelled', error: 'Check your device and try again' } as Record<string, string>)[stage] ?? 'Preparing transfer' }
function message(e: unknown) { return e instanceof Error ? e.message : String(e || 'Something went wrong. Try again.') }
