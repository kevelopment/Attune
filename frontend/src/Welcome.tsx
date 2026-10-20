import { useEffect, useState } from 'react'
import { BookOpen, Check, FolderOpen, Search } from 'lucide-react'
import type { ImportResult } from './types'
import './Welcome.css'

export type CalibreLocation = { path: string; name: string }
type Props = {
  libraries: CalibreLocation[]
  error?: string
  onRetry: () => void
  onScan: () => Promise<CalibreLocation[]>
  onComplete: () => Promise<void>
  onImport: (path: string) => Promise<ImportResult | null>
  onBrowse: () => Promise<ImportResult | null>
  preview?: boolean
}

export default function Welcome({ libraries: initialLibraries, error, onRetry, onScan, onComplete, onImport, onBrowse, preview }: Props) {
  const [libraries, setLibraries] = useState(initialLibraries)
  const [scanning, setScanning] = useState(false)
  const [working, setWorking] = useState(false)
  const [scanError, setScanError] = useState('')
  const [result, setResult] = useState<ImportResult | null>(null)
  const [completionError, setCompletionError] = useState('')
  const [completionBusy, setCompletionBusy] = useState(false)
  const [hasScanned, setHasScanned] = useState(false)
  useEffect(() => setLibraries(initialLibraries), [initialLibraries])

  async function complete() {
    setCompletionError('')
    setCompletionBusy(true)
    try { await onComplete() } catch (e) { setCompletionError(e instanceof Error ? e.message : String(e)) }
    finally { setCompletionBusy(false) }
  }
  async function importFrom(action: () => Promise<ImportResult | null>) {
    setWorking(true)
    setResult(null)
    try {
      const next = await action()
      if (!next) return
      setResult(next)
      if (!next.errors?.length && (next.imported > 0 || next.duplicates > 0)) await complete()
    } finally { setWorking(false) }
  }
  async function scan() {
    setScanning(true)
    setScanError('')
    try { setLibraries(await onScan()); setHasScanned(true) } catch (e) { setScanError(e instanceof Error ? e.message : String(e)) }
    finally { setScanning(false) }
  }

  return <main className={`welcome-shell ${preview ? 'preview-mode' : ''}`} aria-labelledby="welcome-title">
    <section className="welcome-panel">
      <img className="welcome-app-icon" src="/attune.svg" alt="" />
      <div className="welcome-intro">
        <h1 id="welcome-title">Attune your library.</h1>
        <p>Start with an empty shelf, or bring in a Calibre library. Your original files stay where they are.</p>
      </div>

      {error ? <div className="welcome-message" role="alert"><span>{error}</span><button className="welcome-button" onClick={onRetry}>Try again</button></div> : <>
        <div className="welcome-choices">
          <article className="welcome-choice">
            <div className="welcome-choice-icon"><BookOpen size={19}/></div>
            <div className="welcome-choice-copy"><h2>Start from scratch</h2><p>Make a fresh library and add books whenever you’re ready.</p></div>
            <button className="welcome-button welcome-primary" disabled={working || completionBusy} aria-busy={completionBusy} onClick={() => void complete()}>{completionBusy ? 'Setting up…' : 'Get started'}</button>
          </article>
          <article className="welcome-choice welcome-import-choice">
            <div className="welcome-choice-heading"><div className="welcome-choice-icon"><FolderOpen size={19}/></div><div className="welcome-choice-copy"><h2>Import Calibre</h2><p>Copy books, metadata and covers into Attune.</p></div></div>
            {libraries.length > 0 && <div className="welcome-library-list" aria-label="Calibre libraries found">{libraries.map(item => <div className="welcome-library" key={item.path}><div><strong>{item.name}</strong><span title={item.path}>{item.path}</span></div><button className="welcome-button" disabled={working || completionBusy} onClick={() => void importFrom(() => onImport(item.path))}>Import</button></div>)}</div>}
            <div className="welcome-import-actions">
              <button className="welcome-button" disabled={working || completionBusy} onClick={() => void importFrom(onBrowse)}><FolderOpen size={15}/>{working ? 'Importing…' : 'Choose folder'}</button>
              <button className="welcome-text-button" disabled={working || completionBusy || scanning} onClick={() => void scan()}><Search size={15}/>{scanning ? 'Scanning…' : 'Scan for libraries'}</button>
            </div>
            {scanError && <p className="welcome-inline-error" role="alert">{scanError}</p>}
            {result && <div className={`welcome-import-result ${result.errors?.length ? 'has-errors' : ''}`} role="status">
              <span>{result.imported} imported · {result.duplicates} duplicates{result.skipped ? ` · ${result.skipped} skipped` : ''}{result.errors?.length ? ` · ${result.errors.length} couldn’t be imported` : ''}</span>
              {result.errors?.[0] && <small>{result.errors[0]}</small>}
              {(result.errors?.length || (!result.imported && !result.duplicates)) && <button className="welcome-button welcome-primary" disabled={working || completionBusy} aria-busy={completionBusy} onClick={() => void complete()}><Check size={15}/>{completionBusy ? 'Continuing…' : 'Continue to library'}</button>}
            </div>}
          </article>
        </div>
        {completionError && <p className="welcome-inline-error welcome-completion-error" role="alert">{completionError}</p>}
        <p className="welcome-footnote">{hasScanned && !libraries.length && 'No Calibre libraries found. '}Scan searches your home folders; choose a folder for libraries elsewhere.</p>
      </>}
    </section>
  </main>
}
