import { useCallback, useEffect, useRef, useState } from 'react'
import { CheckCircle2, Download, LoaderCircle, RefreshCw, RotateCw } from 'lucide-react'
import { version } from '../package.json'
import { bridge } from './bridge'
import type { UpdateState } from './types'
import { InlineBanner, useNotifications } from './Notifications'
import './Updates.css'

const initialState: UpdateState = { currentVersion: version, latestVersion: '', available: false, downloaded: false, status: 'idle', releaseURL: '', releaseNotes: '', progress: 0, received: 0, total: 0, error: '' }
const failure = (error: unknown) => error instanceof Error ? error.message : String(error)
const UPDATE_CHECK_INTERVAL = 15 * 60 * 1000

export function useUpdater() {
  const [state, setState] = useState<UpdateState>(initialState)
  const [checked, setChecked] = useState(false)
  const { notify } = useNotifications()
  const checking = useRef(false)
  const lastCheck = useRef(0)
  const active = useRef(false)

  const check = useCallback(async (manual = true) => {
    if (checking.current || (!manual && Date.now() - lastCheck.current < UPDATE_CHECK_INTERVAL)) return
    checking.current = true
    lastCheck.current = Date.now()
    try { const next = await bridge.checkForUpdates(); if (active.current) { setState(next); setChecked(true) } }
    catch (error) {
      if (active.current) setState(current => ({ ...current, status: 'error', error: failure(error) }))
      if (manual) notify({ text: failure(error), kind: 'error' })
    } finally { checking.current = false }
  }, [notify])

  useEffect(() => {
    active.current = true
    const unsubscribe = bridge.on('update:state', (next: UpdateState) => { if (active.current) setState(next) })
    void bridge.getUpdateState().then(next => { if (active.current) { setState(next); if (!next.downloaded) void check(false) } }).catch(() => { if (active.current) void check(false) })
    const focus = () => { if (document.visibilityState === 'visible') void check(false) }
    window.addEventListener('focus', focus)
    const timer = window.setInterval(() => void check(false), UPDATE_CHECK_INTERVAL)
    return () => { active.current = false; if (typeof unsubscribe === 'function') unsubscribe(); window.removeEventListener('focus', focus); window.clearInterval(timer) }
  }, [check])

  const download = useCallback(async () => {
    try {
      const next = await bridge.downloadUpdate()
      if (active.current) setState(next)
      if (next.downloaded) notify({ text: 'Update downloaded. It will be active the next time you open Attune.', kind: 'success' })
      return next
    } catch (error) {
      if (active.current) setState(current => ({ ...current, status: 'error', error: failure(error) }))
      notify({ text: failure(error), kind: 'error' })
      return null
    }
  }, [notify])

  const restart = useCallback(async () => {
    try { await bridge.restartNow() }
    catch (error) { notify({ text: failure(error), kind: 'error' }); if (active.current) setState(current => ({ ...current, status: 'ready', error: failure(error) })) }
  }, [notify])

  return { state, checked, check, download, restart }
}

export function UpdateSettingsCard({ state, onOpen }: { state: UpdateState; onOpen: () => void }) {
  return <div className="settings-card"><div><strong>Software updates</strong><span>Attune {state.currentVersion}{state.downloaded ? ' · Ready to restart' : state.available ? ' · Update available' : ''}</span></div><button className="toolbar-button" onClick={onOpen}><Download size={16}/> {state.downloaded ? 'Review update' : state.available ? 'Update available' : 'Check for updates'}</button></div>
}

export default function Updates({ state, checked, onCheck, onDownload, onRestart, restartDisabled }: { state: UpdateState; checked: boolean; onCheck: () => void; onDownload: () => void; onRestart: () => void; restartDisabled: boolean }) {
  const pending = state.status === 'checking' || state.status === 'downloading' || state.status === 'restarting'
  const title = state.downloaded ? 'Ready for your next chapter' : state.available ? 'A new version is available' : state.status === 'checking' ? 'Checking for updates…' : checked && state.status === 'idle' ? 'You’re up to date' : 'Software updates'
  return <section className="settings-page update-page">
    <div className="settings-heading"><div className="settings-icon"><Download size={22}/></div><div><p>A little care for your library companion.</p></div></div>
    <div className="settings-card updates-card">
      <div className="update-summary"><strong>{title}</strong><span>Installed version {state.currentVersion}{state.latestVersion ? ` · ${state.available ? 'Available' : 'Latest'} version ${state.latestVersion}` : ''}</span>
        {state.downloaded ? <p>The update is downloaded and verified. It will install when you quit Attune and be active the next time you open the app.</p> : <p>Updates come directly from Attune’s GitHub releases. Your library stays in place.</p>}
      </div>
      <div className="settings-actions">
        {state.downloaded ? <button className="toolbar-button primary-button" disabled={restartDisabled || pending} onClick={onRestart}><RotateCw size={16}/> Restart now</button> : state.status === 'downloading' ? <button className="toolbar-button" disabled><LoaderCircle size={16} className="spinning"/> Downloading…</button> : state.available ? <button className="toolbar-button primary-button" disabled={pending} onClick={onDownload}><Download size={16}/> Download update</button> : <button className="toolbar-button" disabled={pending} onClick={onCheck}><RefreshCw size={16} className={state.status === 'checking' ? 'spinning' : ''}/> {state.status === 'checking' ? 'Checking…' : 'Check for updates'}</button>}
      </div>
    </div>
    {state.status === 'downloading' && <div className="update-download" role="status"><div className="update-progress-label"><span>Downloading Attune {state.latestVersion}</span><span>{Math.round(state.progress || 0)}%</span></div><progress aria-label="Update download progress" max={100} value={state.progress || 0}/></div>}
    {state.downloaded && <div className="settings-note"><CheckCircle2 size={16}/><span>{restartDisabled ? 'Finish the current import or transfer before restarting.' : 'You can keep using Attune and restart later.'}</span></div>}
    {state.error && <InlineBanner>{state.error}</InlineBanner>}
    {state.latestVersion && state.releaseNotes && <section className="update-release-notes" aria-label="Release notes"><h2>What’s new in {state.latestVersion}</h2><p>{state.releaseNotes}</p></section>}
  </section>
}
