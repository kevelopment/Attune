import { Scaling } from 'lucide-react'
import './GallerySize.css'

export default function BookSizeControl({ value, onChange }: { value: number; onChange: (value: number) => void }) {
  return <label className="book-size-control" title={`Book size: ${value}%`}>
    <Scaling size={15} aria-hidden="true"/>
    <input type="range" aria-label="Book size" aria-valuetext={`${value}%`} min="80" max="140" step="10" value={value} onChange={event => onChange(Number(event.currentTarget.value))}/>
  </label>
}
