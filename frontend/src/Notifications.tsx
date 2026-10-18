import { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { AlertCircle, CheckCircle2, Info, X } from 'lucide-react'

type Notification = { id: number; text: string; kind: 'success' | 'status' | 'error' }
type NotificationContextValue = {
  notify: (notification: Omit<Notification, 'id'>) => void
  dismiss: () => void
}

const NotificationContext = createContext<NotificationContextValue | null>(null)

export function NotificationsProvider({ children }: { children: React.ReactNode }) {
  const [current, setCurrent] = useState<Notification | null>(null)
  const nextID = useRef(0)
  const recent = useRef(new Map<string, number>())

  const notify = useCallback((notification: Omit<Notification, 'id'>) => {
    const now = Date.now()
    const key = `${notification.kind}:${notification.text}`
    const lastShown = recent.current.get(key)
    if (lastShown !== undefined && now - lastShown < 30_000) return
    for (const [known, timestamp] of recent.current) if (now - timestamp >= 60_000) recent.current.delete(known)
    recent.current.set(key, now)
    setCurrent({ ...notification, id: nextID.current++ })
  }, [])
  const dismiss = useCallback(() => setCurrent(null), [])

  useEffect(() => {
    if (!current) return
    const timer = window.setTimeout(() => setCurrent(null), current.kind === 'error' ? 8000 : 5000)
    return () => window.clearTimeout(timer)
  }, [current])

  return <NotificationContext.Provider value={{ notify, dismiss }}>
    {children}
    {current && <div className={`toast ${current.kind === 'error' ? 'toast-error' : current.kind === 'status' ? 'toast-status' : ''}`} role={current.kind === 'error' ? 'alert' : 'status'}>
      {current.kind === 'error' ? <AlertCircle size={17} aria-hidden="true"/> : current.kind === 'status' ? <Info size={17} aria-hidden="true"/> : <CheckCircle2 size={17} aria-hidden="true"/>}
      <span>{current.text}</span>
      <button onClick={dismiss} aria-label="Dismiss message"><X size={15}/></button>
    </div>}
  </NotificationContext.Provider>
}

export function useNotifications() {
  const value = useContext(NotificationContext)
  if (!value) throw new Error('useNotifications must be used inside NotificationsProvider')
  return value
}

export function InlineBanner({ children, action, className = '' }: { children: ReactNode; action?: ReactNode; className?: string }) {
  return <div className={`inline-banner ${className}`} role="status">
    <AlertCircle size={15} aria-hidden="true"/><span>{children}</span>{action}
  </div>
}
