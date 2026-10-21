import { forwardRef, useEffect, useImperativeHandle, useRef } from 'react'
import { bridge } from '../bridge'
import type { BookFile, ReadingProgress } from '../types'
import { createReaderAdapter } from './index'
import type { ReaderLocator, ReaderSearchHit, ReaderSection, ReaderSettings } from './adapter'

const OPEN_TIMEOUT_MS = 90_000

export type ReaderSurfaceHandle = {
  next(): void
  previous(): void
  navigate(index: number): void
  location(): ReaderLocator | null
  resolve(locator: ReaderLocator): Promise<boolean>
  search(query: string): Promise<ReaderSearchHit[]>
  goToSearchHit(hit: ReaderSearchHit): void
  restore(locator: ReaderLocator): Promise<boolean>
}

type Props = {
  bookID: string
  file: BookFile
  legacyProgress: Pick<ReadingProgress, 'chapterIndex' | 'position'>
  settings: ReaderSettings
  onSections(sections: ReaderSection[]): void
  onLocation(locator: ReaderLocator, sectionIndex: number): void
  onError(message: string): void
  onLoading(loading: boolean): void
}

export const ReaderSurface = forwardRef<ReaderSurfaceHandle, Props>(function ReaderSurface(
  { bookID, file, legacyProgress, settings, onSections, onLocation, onError, onLoading },
  ref,
) {
  const host = useRef<HTMLDivElement>(null)
  const adapter = useRef<ReturnType<typeof createReaderAdapter>>(null)
  const callbacks = useRef({ onSections, onLocation, onError, onLoading })
  const saveTimer = useRef<number | undefined>(undefined)
  const settingsRef = useRef(settings)
  callbacks.current = { onSections, onLocation, onError, onLoading }
  settingsRef.current = settings

  useImperativeHandle(ref, () => ({
    next: () => { void adapter.current?.next() },
    previous: () => { void adapter.current?.previous() },
    navigate: index => { void adapter.current?.navigate(index) },
    location: () => adapter.current?.location() ?? null,
    resolve: async locator => adapter.current?.resolve(locator) ?? false,
    search: async query => adapter.current?.search(query) ?? [],
    goToSearchHit: hit => { void adapter.current?.goToSearchHit(hit) },
    restore: async locator => adapter.current?.restore(locator) ?? false,
  }), [])

  useEffect(() => {
    const container = host.current
    const instance = createReaderAdapter(file.format)
    if (!container || !instance) {
      callbacks.current.onError(`Attune cannot read ${file.format.toUpperCase()} files yet.`)
      callbacks.current.onLoading(false)
      return
    }
    const controller = new AbortController()
    let live = true
    let openTimeout: number | undefined
    let lastLocator: ReaderLocator | null = null
    let lastSectionIndex = 0
    adapter.current = instance
    callbacks.current.onLoading(true)
    callbacks.current.onError('')
    const unsubscribe = instance.on(event => {
      if (!live) return
      if (event.type === 'sections') callbacks.current.onSections(event.sections)
      else if (event.type === 'location') {
        lastLocator = event.locator
        lastSectionIndex = event.sectionIndex
        callbacks.current.onLocation(event.locator, event.sectionIndex)
        window.clearTimeout(saveTimer.current)
        saveTimer.current = window.setTimeout(() => {
          void bridge.saveReaderLocation(bookID, file.id, event.locator.version, event.locator).catch(() => {})
          void bridge.saveReadingProgress(bookID, event.sectionIndex, event.locator.progress).catch(() => {})
        }, 450)
      } else {
        callbacks.current.onLoading(false)
        callbacks.current.onError(event.message)
      }
    })
    openTimeout = window.setTimeout(() => {
      if (!live) return
      live = false
      controller.abort()
      unsubscribe()
      window.clearTimeout(saveTimer.current)
      instance.dispose()
      if (adapter.current === instance) adapter.current = null
      callbacks.current.onLoading(false)
      callbacks.current.onError('This book did not finish opening. Close it and try again, or try another format.')
    }, OPEN_TIMEOUT_MS)

    const source = file.format === 'cbz' || file.format === 'cbr'
      ? bridge.bookArchiveURL(bookID, file.id)
      : bridge.bookFileURL(bookID, file.id)

    void (async () => {
      try {
        await instance.open(container, source, settingsRef.current, controller.signal)
        if (!live) return
        const saved = await bridge.getReaderLocation(bookID, file.id).catch(() => null)
        const legacy = await bridge.getReadingProgress(bookID).catch(() => legacyProgress)
        let restored = false
        if (saved?.found && saved.schemaVersion === 1 && saved.locator && typeof saved.locator === 'object') {
          restored = await instance.restore(saved.locator as ReaderLocator).catch(() => false)
        }
        if (!restored && file.format === 'epub' && instance.sections.length > 0) {
          const fraction = Math.min(1, Math.max(0, (legacy.chapterIndex + legacy.position) / instance.sections.length))
          const legacyLocator: ReaderLocator = { version: 1, kind: 'text-offset', value: 0, progress: fraction }
          restored = await instance.restore(legacyLocator).catch(() => false)
        }
        if (live) {
          callbacks.current.onLoading(false)
          if (lastLocator) callbacks.current.onLocation(lastLocator, lastSectionIndex)
        }
      } catch (error) {
        if (!live || controller.signal.aborted) return
        callbacks.current.onLoading(false)
        callbacks.current.onError(error instanceof Error ? error.message : 'Could not open this book.')
      } finally {
        if (openTimeout !== undefined) window.clearTimeout(openTimeout)
      }
    })()

    return () => {
      live = false
      if (openTimeout !== undefined) window.clearTimeout(openTimeout)
      controller.abort()
      unsubscribe()
      window.clearTimeout(saveTimer.current)
      if (lastLocator) void bridge.saveReaderLocation(bookID, file.id, lastLocator.version, lastLocator).catch(() => {})
      instance.dispose()
      if (adapter.current === instance) adapter.current = null
    }
  }, [bookID, file.id, file.format])

  useEffect(() => { adapter.current?.applySettings(settings) }, [settings])

  return <div className="reader-engine-surface" ref={host} aria-label="Book pages" />
})
