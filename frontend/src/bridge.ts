import type { AppState, Book, BookMetadataEdit, BookPreview, Device, DeviceBook, ImportResult, Progress, ImportProgress, UpdateState } from './types'

const isPreview = new URLSearchParams(location.search).has('preview') || new URLSearchParams(location.search).get('welcome') === '1' || (import.meta.env.DEV && import.meta.env.VITE_ATTUNE_PREVIEW === '1')
const previewStateError = new URLSearchParams(location.search).get('state') === 'error'
const previewWelcome = new URLSearchParams(location.search).get('welcome') === '1'
const previewEjectFails = new URLSearchParams(location.search).get('eject') === 'fail'
const previewEjectDelay = new URLSearchParams(location.search).get('eject') === 'slow' ? 8000 : 1400
const coverColors = ['#797a66', '#577067', '#bb7b55', '#586987', '#986b7b', '#8d7258']
const sampleTitles = ['The Silent Library', 'A Field Guide to Elsewhere', 'Light Between Worlds', 'Small Hours', 'The Art of Getting Lost', 'A Map of Quiet Places', 'Under the Fig Tree', 'The Long Way Home', 'Ordinary Magic', 'Letters from the Coast']
const sampleAuthors = ['Mira Ellison', 'Jonas Vale', 'Amara Sato', 'N. R. Finch', 'Clara Moss', 'Leo Winter']
const previewBooks: Book[] = sampleTitles.map((title, i) => {
  const batch = Math.floor(i / 3)
  const added = new Date()
  added.setDate(added.getDate() - batch)
  added.setTime(added.getTime() - (i % 3) * 1000)
  return { id: `sample-${i}`, title, authors: [sampleAuthors[i % sampleAuthors.length]], language: 'en', sha256: '', coverURL: `preview:${i}`, addedAt: added.toISOString(), importBatchID: `sample-batch-${batch}`, revision: 1 }
})
const previewState: AppState = { books: previewWelcome ? [] : previewBooks, devices: [{ id: 'kindle-preview', name: 'Kindle', root: '/Volumes/Kindle', freeBytes: 842_000_000, totalBytes: 1_600_000_000 }], converterReady: true, converterVersion: 'preview', libraryPath: '~/Library/Application Support/Ebook Library' }
const previewCalibreLibraries = [{ path: '~/Books/Calibre Library', name: 'Calibre Library' }]
const updateScenario = new URLSearchParams(location.search).get('update')
let previewUpdate: UpdateState = { currentVersion: '0.0.5', latestVersion: updateScenario && updateScenario !== 'latest' ? '0.0.6' : '', available: !!updateScenario && updateScenario !== 'latest', downloaded: updateScenario === 'ready', status: updateScenario === 'ready' ? 'ready' : updateScenario === 'error' ? 'error' : updateScenario === 'available' ? 'available' : 'idle', releaseURL: 'https://github.com/kevelopment/Attune/releases', releaseNotes: 'A few thoughtful improvements for your library.', progress: updateScenario === 'ready' ? 100 : 0, received: 0, total: 0, error: updateScenario === 'error' ? 'The update could not be downloaded. Please try again.' : '' }
let previewUpdateAttempts = 0
let updateListeners: ((state: UpdateState) => void)[] = []
function emitUpdate(patch: Partial<UpdateState>) { previewUpdate = { ...previewUpdate, ...patch }; updateListeners.forEach(listener => listener({ ...previewUpdate })) }
const previewDevice: Device = { ...previewState.devices[0] }
let listeners: ((p: Progress) => void)[] = []
let importListeners: ((p: ImportProgress) => void)[] = []
let previewTransfer: Progress | null = null
let previewDeviceBooks: DeviceBook[] = previewBooks.slice(0, 6).map((book, i) => ({ path: `documents/${book.title}.mobi`, name: `${book.title}.mobi`, size: 257000 + i * 113000, bookID: book.id, title: book.title, authors: book.authors, language: book.language, coverURL: book.coverURL }))

function api(name: string, ...args: unknown[]) {
  const fn = window.go?.main?.App?.[name]
  if (!fn) throw new Error(`The desktop command “${name}” is unavailable. Run this app in its desktop window.`)
  return fn(...args)
}

export const bridge = {
  preview: isPreview,
  async getOnboardingState(): Promise<{ required: boolean; libraries: { path: string; name: string }[] }> {
    return isPreview ? { required: previewWelcome, libraries: previewWelcome ? previewCalibreLibraries : [] } : api('GetOnboardingState')
  },
  async completeOnboarding(): Promise<void> {
    if (!isPreview) return api('CompleteOnboarding')
  },
  async findCalibreLibraries(scan: boolean): Promise<{ path: string; name: string }[]> {
    if (!isPreview) return api('FindCalibreLibraries', scan)
    if (!scan) return previewCalibreLibraries
    await new Promise(resolve => window.setTimeout(resolve, 650))
    return [...previewCalibreLibraries, { path: '~/Documents/Reader Archive', name: 'Reader Archive' }]
  },
  async importCalibreLibraryAt(path: string): Promise<ImportResult> {
    if (!isPreview) return api('ImportCalibreLibraryAt', path)
    await new Promise(resolve => window.setTimeout(resolve, 800))
    if (previewWelcome) previewState.books = previewBooks
    return { imported: 12, duplicates: 2, skipped: 1, errors: [], bookIDs: ['sample-0', 'sample-1'] }
  },
  async getUpdateState(): Promise<UpdateState> { return isPreview ? { ...previewUpdate } : api('GetUpdateState') },
  async checkForUpdates(): Promise<UpdateState> {
    if (!isPreview) return api('CheckForUpdates')
    if (previewUpdate.downloaded) return { ...previewUpdate }
    emitUpdate({ status: 'checking', error: '' })
    await new Promise(resolve => window.setTimeout(resolve, 300))
    emitUpdate({ status: previewUpdate.available ? 'available' : 'idle' })
    return { ...previewUpdate }
  },
  async downloadUpdate(): Promise<UpdateState> {
    if (!isPreview) return api('DownloadUpdate')
    const failDownload = updateScenario === 'error' && previewUpdateAttempts++ === 0
    emitUpdate({ status: 'downloading', error: '', progress: 0, total: 100 })
    for (let received = 10; received <= 100; received += 10) {
      await new Promise(resolve => window.setTimeout(resolve, 120))
      if (failDownload && received === 30) { const error = 'The download was interrupted. Please try again.'; emitUpdate({ status: 'error', error }); throw new Error(error) }
      emitUpdate({ received, progress: received })
    }
    emitUpdate({ status: 'ready', downloaded: true })
    return { ...previewUpdate }
  },
  async restartNow(): Promise<void> {
    if (!isPreview) return api('RestartNow')
    emitUpdate({ status: 'restarting' })
    await new Promise(resolve => window.setTimeout(resolve, 400))
    emitUpdate({ status: 'idle', currentVersion: previewUpdate.latestVersion, available: false, downloaded: false })
  },
  async getState(): Promise<AppState> {
    if (isPreview && previewStateError) throw new Error('Could not refresh library. Try again.')
    return isPreview ? { ...previewState, books: [...previewState.books], devices: [...previewState.devices] } : api('GetState')
  },
  async importBooks(): Promise<ImportResult> {
    if (!isPreview) return api('ImportBooks')
    return { imported: 2, duplicates: 1, errors: [], bookIDs: ['sample-0', 'sample-1'] }
  },
  async importCalibreLibrary(): Promise<ImportResult> {
    if (!isPreview) return api('ImportCalibreLibrary')
    if (previewWelcome) previewState.books = previewBooks
    return { imported: 2, duplicates: 1, skipped: 1, errors: [], bookIDs: ['sample-0', 'sample-1'] }
  },
  async readClipboardFiles(): Promise<string[]> {
    if (!isPreview) return api('ReadClipboardFiles')
    return ['~/Downloads/Piranesi.epub', '~/Downloads/The Silent Library.epub']
  },
  async importPaths(paths: string[]): Promise<ImportResult> {
    if (!isPreview) return api('ImportPaths', paths)
    let imported = 0
    let duplicates = 0
    const importBatchID = crypto.randomUUID()
    const bookIDs = paths.map((path, i) => {
      const title = fileTitle(path)
      const existing = previewState.books.find(book => book.title.toLowerCase() === title.toLowerCase())
      if (existing) { duplicates++; return existing.id }
      const id = `preview-import-${Date.now()}-${i}`
      previewState.books = [...previewState.books, { id, title, authors: [], language: '', sha256: '', coverURL: `preview:${i}`, addedAt: new Date().toISOString(), importBatchID, revision: 1 }]
      imported++
      return id
    })
    return { imported, duplicates, errors: [], bookIDs: [...new Set(bookIDs)] }
  },
  async previewImport(): Promise<ImportResult> {
    const importBatchID = crypto.randomUUID()
    const files = ['The Garden of Small Beginnings.epub', 'Piranesi.epub', 'The City and Its Uncertain Walls.epub', 'The Midnight Library.epub', 'The Thursday Murder Club.epub', 'A Psalm for the Wild-Built.epub', 'The Overstory.epub', 'Circe.epub', 'The Book of Delights.epub', 'Station Eleven.epub', 'The Ocean at the End of the Lane.epub', 'Braiding Sweetgrass.epub', 'The House in the Cerulean Sea.epub', 'Never Let Me Go.epub', 'The Left Hand of Darkness.epub', 'The Dispossessed.epub', 'A duplicate title.epub', 'A damaged book.epub']
    for (let i = 0; i < files.length; i++) {
      await new Promise(resolve => window.setTimeout(resolve, 180))
      const imported = Math.min(i + 1, 16)
      const duplicates = i >= 16 ? 1 : 0
      const failed = i >= 17 ? 1 : 0
      const p: ImportProgress = { current: i + 1, total: files.length, filename: files[i], imported, duplicates, failed, stage: i === files.length - 1 ? 'done' : 'importing' }
      importListeners.forEach(fn => fn(p))
    }
    const titles = files.slice(0, 16).map(name => name.replace(/\.epub$/i, ''))
    previewState.books = [...previewState.books, ...titles.map((title, i) => ({ id: `preview-import-${Date.now()}-${i}`, title, authors: [sampleAuthors[i % sampleAuthors.length]], language: 'en', sha256: '', coverURL: `preview:${i}`, addedAt: new Date().toISOString(), importBatchID, revision: 1 }))]
    return { imported: 16, duplicates: 1, errors: ['The damaged book could not be read.'], bookIDs: [...new Set(previewState.books.filter(book => titles.includes(book.title)).map(book => book.id))] }
  },
  async updateBook(id: string, title: string, authors: string[]): Promise<Book> {
    if (!isPreview) return api('UpdateBook', id, title, authors)
    const found = previewState.books.find(b => b.id === id)!
    Object.assign(found, { title, authors })
    return found
  },
  async changeBookCover(id: string, file?: File): Promise<Book | null> {
    if (!isPreview) return api('ChangeBookCover', id)
    if (!file) return null
    if (!file.size || file.size > 32 * 1024 * 1024) throw new Error('Choose a JPEG or PNG image smaller than 32 MB.')
    const header = new Uint8Array(await file.slice(0, 8).arrayBuffer())
    const png = [137, 80, 78, 71, 13, 10, 26, 10].every((byte, index) => header[index] === byte)
    const jpeg = header[0] === 255 && header[1] === 216 && header[2] === 255
    if (!png && !jpeg) throw new Error('Choose a JPEG or PNG image.')
    const coverURL = await new Promise<string>((resolve, reject) => {
      const reader = new FileReader()
      reader.onload = () => resolve(String(reader.result))
      reader.onerror = () => reject(new Error('The cover image could not be read.'))
      reader.readAsDataURL(file)
    })
    await new Promise<void>((resolve, reject) => {
      const image = new Image()
      image.onload = () => image.naturalWidth * image.naturalHeight <= 40_000_000 ? resolve() : reject(new Error('Choose a cover image with no more than 40 megapixels.'))
      image.onerror = () => reject(new Error('The cover image is damaged or unreadable.'))
      image.src = coverURL
    })
    const found = previewState.books.find(book => book.id === id)
    if (!found) throw new Error('This book is no longer in your library.')
    const updated = { ...found, coverURL, revision: found.revision + 1 }
    previewState.books = previewState.books.map(book => book.id === id ? updated : book)
    return updated
  },
  async updateBookMetadata(id: string, edit: BookMetadataEdit): Promise<Book> {
    if (!isPreview) return api('UpdateBookMetadata', id, edit)
    const found = previewState.books.find(b => b.id === id)
    if (!found) throw new Error('This book is no longer in your library.')
    Object.assign(found, { ...edit, authors: [...edit.authors], subjects: [...edit.subjects] })
    return { ...found }
  },
  async deleteBook(id: string): Promise<void> {
    if (!isPreview) return api('DeleteBook', id)
    previewState.books = previewState.books.filter(b => b.id !== id)
  },
  async selectDevice(): Promise<Device | null> {
    if (!isPreview) return api('SelectDevice')
    if (!previewState.devices.some(device => device.id === previewDevice.id)) previewState.devices = [...previewState.devices, previewDevice]
    return previewDevice
  },
  async getDeviceBooks(id: string): Promise<DeviceBook[]> { return isPreview ? previewDeviceBooks : api('GetDeviceBooks', id) },
  async getBookPreview(id: string): Promise<BookPreview> {
    if (!isPreview) return api('GetBookPreview', id)
    const book = previewState.books.find(candidate => candidate.id === id)
    if (!book) throw new Error('This book is no longer in your library.')
    return { book, chapters: [{ id: 'preview-chapter-1', title: 'Chapter One', href: '/preview/sample/chapter-1.xhtml' }, { id: 'preview-chapter-2', title: 'The next morning', href: '/preview/sample/chapter-2.xhtml' }, { id: 'preview-chapter-3', title: 'A small discovery', href: '/preview/sample/chapter-3.xhtml' }] }
  },
  async sendBooks(deviceID: string, bookIDs: string[]): Promise<void> {
    if (!isPreview) return api('SendBooks', deviceID, bookIDs)
    const selected = previewState.books.filter(book => bookIDs.includes(book.id))
    if (!selected.length) return
    const total = selected.length
    previewTransfer = { id: 'preview-transfer', bookID: selected[0].id, title: total === 1 ? selected[0].title : `${total} books`, stage: 'transferring', message: 'Copying to Kindle', current: 0, total }
    listeners.forEach(fn => fn(previewTransfer!))
    for (let current = 1; current <= total; current++) {
      await new Promise(resolve => window.setTimeout(resolve, 450))
      if (!previewTransfer || previewTransfer.stage === 'cancelled') return
      previewTransfer = { ...previewTransfer, bookID: selected[current - 1].id, title: selected[current - 1].title, current, total, stage: current === total ? 'done' : 'transferring', message: current === total ? 'Saved to Kindle' : 'Copying to Kindle' }
      listeners.forEach(fn => fn(previewTransfer!))
    }
    previewTransfer = null
  },
  async cancelTransfer(): Promise<void> {
    if (!isPreview) return api('CancelTransfer')
    if (previewTransfer) {
      previewTransfer = { ...previewTransfer, stage: 'cancelled', message: 'Transfer cancelled' }
      listeners.forEach(fn => fn(previewTransfer!))
      previewTransfer = null
    }
  },
  async removeDeviceBooks(id: string, paths: string[]): Promise<void> {
    if (!isPreview) return api('RemoveDeviceBooks', id, paths)
    previewDeviceBooks = previewDeviceBooks.filter(b => !paths.includes(b.path))
  },
  async ejectDevice(id: string): Promise<void> {
    if (!isPreview) return api('EjectDevice', id)
    await new Promise(resolve => window.setTimeout(resolve, previewEjectDelay))
    if (previewEjectFails) throw new Error('Could not safely eject the Kindle. Check the connection and try again.')
    previewState.devices = previewState.devices.filter(device => device.id !== id)
  },
  async openLibraryFolder(): Promise<void> { if (!isPreview) return api('OpenLibraryFolder') },
  async changeLibraryLocation(): Promise<string> {
    if (!isPreview) return api('ChangeLibraryLocation')
    await new Promise(resolve => window.setTimeout(resolve, 600))
    previewState.libraryPath = '~/Documents/Attune Library'
    return previewState.libraryPath
  },
  on(event: string, cb: (...args: any[]) => void) {
    if (isPreview) {
      if (event === 'transfer:progress') listeners.push(cb)
      if (event === 'import:progress') importListeners.push(cb)
      if (event === 'update:state') updateListeners.push(cb)
      return () => { listeners = listeners.filter(fn => fn !== cb); importListeners = importListeners.filter(fn => fn !== cb); updateListeners = updateListeners.filter(fn => fn !== cb) }
    }
    return window.runtime?.EventsOn?.(event, cb)
  },
  previewCover(url: string) {
    if (!url.startsWith('preview:')) return url
    const i = Number(url.split(':')[1]) || 0
    const title = sampleTitles[i] ?? 'A Good Book'
    const color = coverColors[i % coverColors.length]
    const fontSize = 36
    const maxWidth = 340
    const escape = (value: string) => value.replace(/[&<>"']/g, char => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&apos;' })[char]!)
    const wrap = (value: string) => {
      const lines: string[] = []
      let line = ''
      for (const word of value.split(/\s+/)) {
        const candidate = line ? `${line} ${word}` : word
        if (line && candidate.length * fontSize * 0.55 > maxWidth) { lines.push(line); line = word }
        else line = candidate
      }
      if (line) lines.push(line)
      return lines
    }
    const lines = wrap(title)
    const titleY = 320 - (lines.length - 1) * 22
    const titleMarkup = lines.map((line, n) => `<tspan x="210" dy="${n ? 44 : 0}" textLength="${Math.min(maxWidth, line.length * fontSize * 0.55)}" lengthAdjust="spacingAndGlyphs">${escape(line)}</tspan>`).join('')
    const artwork = i % 3 === 0
      ? `<circle cx="210" cy="166" r="66" fill="none" stroke="#ffffff42" stroke-width="1.5"/><circle cx="210" cy="166" r="51" fill="none" stroke="#ffffff2b"/><path d="M210 88v156M132 166h156" stroke="#ffffff35"/>`
      : i % 3 === 1
        ? `<path d="M70 142c40-48 85-48 125 0s85 48 155 0M70 166c40-48 85-48 125 0s85 48 155 0M70 190c40-48 85-48 125 0s85 48 155 0" fill="none" stroke="#ffffff38" stroke-width="1.5"/>`
        : `<path d="M128 240V164a82 82 0 0 1 164 0v76M150 240v-76a60 60 0 0 1 120 0v76" fill="none" stroke="#ffffff42" stroke-width="1.5"/><circle cx="210" cy="159" r="15" fill="#ffffff28"/>`
    const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="420" height="620" viewBox="0 0 420 620"><rect width="420" height="620" fill="${color}"/><rect x="17" y="17" width="386" height="586" fill="none" stroke="#ffffff55"/><text x="210" y="${titleY}" fill="#fff" text-anchor="middle" font-family="Georgia" font-size="${fontSize}" font-weight="bold">${titleMarkup}</text><g>${artwork}</g><text x="210" y="545" fill="#ffffffcc" text-anchor="middle" font-family="sans-serif" font-size="14" letter-spacing="3">${escape(sampleAuthors[i % sampleAuthors.length].toUpperCase())}</text></svg>`
    return `data:image/svg+xml,${encodeURIComponent(svg)}`
  },
}

function fileTitle(path: string) { return path.split(/[\\/]/).pop()?.replace(/\.[^.]+$/, '') || 'Imported book' }

export function progressFromEvent(value: any): Progress { return value as Progress }
