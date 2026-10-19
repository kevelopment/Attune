import { Scaling } from 'lucide-react'
import type { CSSProperties } from 'react'
import { bookSizeRange } from './appearance'
import './GallerySize.css'

export default function BookSizeControl({ value, onChange }: { value: number; onChange: (value: number) => void }) {
  const displayedValue = `${Math.round(value)}%`
  const progress = ((value - bookSizeRange.min) / (bookSizeRange.max - bookSizeRange.min)) * 100
  return <label className="book-size-control" title={`Book size: ${displayedValue}`}>
    <Scaling size={15} aria-hidden="true"/>
    <input type="range" aria-label="Book size" aria-valuetext={displayedValue} min={bookSizeRange.min} max={bookSizeRange.max} step="any" value={value} style={{ '--range-progress': `${progress}%` } as CSSProperties} onChange={event => onChange(Number(event.currentTarget.value))}/>
  </label>
}
