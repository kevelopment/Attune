import { useCallback, useEffect, useRef, useState } from 'react'
import type { KeyboardEvent } from 'react'
import { AlertCircle, Check, Clock3, LoaderCircle, X } from 'lucide-react'
import './ActivityCenter.css'

export type ActivityEntry = {
  id: string
  kind: 'import' | 'transfer' | 'eject'
  status: 'running' | 'success' | 'error' | 'cancelled'
  title: string
  detail: string
  current: number
  total: number
  timestamp: number
}

export type ActivityPatch = Partial<Omit<ActivityEntry, 'id' | 'kind'>>

export function useActivityLog() {
  const [entries, setEntries] = useState<ActivityEntry[]>([])
  const nextId = useRef(0)

  const begin = useCallback((kind: ActivityEntry['kind'], title: string) => {
    const id = `activity-${Date.now()}-${nextId.current++}`
    setEntries((current) => [{
      id, kind, status: 'running' as const, title, detail: '', current: 0, total: 0, timestamp: Date.now(),
    }, ...current].slice(0, 50))
    return id
  }, [])

  const update = useCallback((id: string, patch: ActivityPatch) => {
    setEntries((current) => current.map((entry) => entry.id === id ? { ...entry, ...patch } : entry))
  }, [])

  const remove = useCallback((id: string) => {
    setEntries((current) => current.filter((entry) => entry.id !== id))
  }, [])

  return { entries, begin, update, remove }
}

type ActivityCenterProps = {
  entries: ActivityEntry[]
  onCancelTransfer: () => void
}

type PopupPosition = { top: number; left: number; maxHeight: number }

function formatTimestamp(timestamp: number) {
  const date = new Date(timestamp)
  if (Number.isNaN(date.getTime())) return ''
  return new Intl.DateTimeFormat(undefined, { hour: 'numeric', minute: '2-digit' }).format(date)
}

function statusText(status: ActivityEntry['status']) {
  switch (status) {
    case 'running': return 'In progress'
    case 'success': return 'Completed'
    case 'error': return 'Failed'
    case 'cancelled': return 'Cancelled'
  }
}

function StatusIcon({ entry }: { entry: ActivityEntry }) {
  if (entry.status === 'running') return <LoaderCircle className="activity-status-running" aria-hidden="true" />
  if (entry.status === 'success') return <Check className="activity-status-success" aria-hidden="true" />
  if (entry.status === 'error') return <AlertCircle className="activity-status-error" aria-hidden="true" />
  return <X className="activity-status-cancelled" aria-hidden="true" />
}

export default function ActivityCenter({ entries, onCancelTransfer }: ActivityCenterProps) {
  const [open, setOpen] = useState(false)
  const [position, setPosition] = useState<PopupPosition>({ top: 48, left: 8, maxHeight: 480 })
  const rootRef = useRef<HTMLDivElement>(null)
  const triggerRef = useRef<HTMLButtonElement>(null)
  const popupRef = useRef<HTMLDivElement>(null)
  const runningCount = entries.filter((entry) => entry.status === 'running').length
  const activeEntry = entries.find((entry) => entry.status === 'running')
  const latestEntry = entries[0]

  const placePopup = useCallback(() => {
    const rect = triggerRef.current?.getBoundingClientRect()
    if (!rect) return
    const width = Math.min(360, Math.max(280, window.innerWidth - 16))
    const left = Math.max(8, Math.min(rect.right - width, window.innerWidth - width - 8))
    const top = rect.bottom + 8
    const maxHeight = Math.max(0, Math.min(480, window.innerHeight - top - 8))
    setPosition({ top, left, maxHeight })
  }, [])

  const close = useCallback((returnFocus = false) => {
    setOpen(false)
    if (returnFocus) requestAnimationFrame(() => triggerRef.current?.focus())
  }, [])

  useEffect(() => {
    if (!open) return
    placePopup()
    const onPointerDown = (event: PointerEvent) => {
      if (event.target instanceof Node && !rootRef.current?.contains(event.target) && !popupRef.current?.contains(event.target)) {
        close()
      }
    }
    const onResize = () => placePopup()
    document.addEventListener('pointerdown', onPointerDown)
    window.addEventListener('resize', onResize)
    window.addEventListener('scroll', onResize, true)
    return () => {
      document.removeEventListener('pointerdown', onPointerDown)
      window.removeEventListener('resize', onResize)
      window.removeEventListener('scroll', onResize, true)
    }
  }, [open, close, placePopup])

  const onKeyDown = (event: KeyboardEvent) => {
    if (event.key === 'Escape' && open) {
      event.preventDefault()
      event.stopPropagation()
      close(true)
    }
  }

  return (
    <div className="activity-center" ref={rootRef} onKeyDown={onKeyDown}>
      <button
        className={`activity-trigger${runningCount ? ' has-running' : ''}`}
        type="button"
        ref={triggerRef}
        aria-label={runningCount ? `Activity, ${runningCount} in progress${latestEntry ? `, latest ${statusText(latestEntry.status)}` : ''}` : latestEntry ? `Activity history, latest ${statusText(latestEntry.status)}` : 'Activity history, no items yet'}
        aria-haspopup="dialog"
        aria-expanded={open}
        title={runningCount ? `${runningCount} active ${runningCount === 1 ? 'task' : 'tasks'}; latest ${latestEntry ? statusText(latestEntry.status).toLowerCase() : 'in progress'} — open activity` : latestEntry ? `Latest activity ${statusText(latestEntry.status).toLowerCase()} — open activity history` : 'No activity yet — open activity history'}
        onClick={() => setOpen((value) => !value)}
      >
        {activeEntry ? <LoaderCircle className="activity-trigger-spinner" aria-hidden="true" /> : latestEntry ? <StatusIcon entry={latestEntry} /> : <Clock3 aria-hidden="true" />}
        {runningCount > 0 && <span className="activity-trigger-count" aria-hidden="true">{runningCount}</span>}
      </button>
      {open && (
        <div
          className="activity-popover"
          ref={popupRef}
          role="dialog"
          aria-label="Current session activity"
          style={position}
        >
          <div className="activity-popover-header">
            <div>
              <strong>Activity</strong>
              <span>{entries.length === 0 ? 'This session' : `${entries.length} ${entries.length === 1 ? 'item' : 'items'} · this session`}</span>
            </div>
            <button className="activity-close" type="button" aria-label="Close activity" onClick={() => close(true)}><X aria-hidden="true" /></button>
          </div>
          {entries.length === 0 ? (
            <div className="activity-empty"><Clock3 aria-hidden="true" /><span>No activity yet</span><small>Imports, transfers, and ejections will appear here.</small></div>
          ) : (
            <div className="activity-list">
              {entries.map((entry) => {
                const progress = entry.total > 0 ? Math.max(0, Math.min(100, entry.current / entry.total * 100)) : 0
                return (
                  <article className={`activity-item is-${entry.status}`} key={entry.id}>
                    <div className="activity-item-icon"><StatusIcon entry={entry} /></div>
                    <div className="activity-item-content">
                      <div className="activity-item-heading">
                        <strong title={entry.title}>{entry.title}</strong>
                        <time dateTime={Number.isNaN(new Date(entry.timestamp).getTime()) ? undefined : new Date(entry.timestamp).toISOString()}>{formatTimestamp(entry.timestamp)}</time>
                      </div>
                      <div className="activity-item-meta">
                        <span>{statusText(entry.status)}</span>
                        {entry.status === 'running' && entry.total > 0 && <span>{Math.max(0, entry.current)} / {entry.total}</span>}
                      </div>
                      {entry.status === 'running' && (
                        <div className="activity-progress" role="progressbar" aria-label={`${entry.title} progress`} aria-valuemin={0} aria-valuemax={entry.total || undefined} aria-valuenow={entry.total > 0 ? Math.max(0, Math.min(entry.current, entry.total)) : undefined}>
                          <span className={entry.total > 0 ? '' : 'is-indeterminate'} style={entry.total > 0 ? { width: `${progress}%` } : undefined} />
                        </div>
                      )}
                      {entry.detail && <p>{entry.detail}</p>}
                      {entry.status === 'running' && entry.kind === 'transfer' && (
                        <button className="activity-cancel" type="button" onClick={onCancelTransfer}>Cancel transfer</button>
                      )}
                    </div>
                  </article>
                )
              })}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
