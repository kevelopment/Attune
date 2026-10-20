import { FixedLayoutAdapter } from './fixed/adapter'
import type { ReaderAdapter, ReflowableFormat } from './adapter'
import { createReflowableAdapter } from './reflowable'

export function createReaderAdapter(format: string): ReaderAdapter | null {
  if (format === 'pdf' || format === 'cbz' || format === 'cbr') return new FixedLayoutAdapter(format)
  if (['epub', 'mobi', 'azw', 'azw3', 'fb2', 'txt'].includes(format)) {
    return createReflowableAdapter(format as ReflowableFormat)
  }
  return null
}

export type { ReaderAdapter, ReaderFormat, ReaderLocator, ReaderSearchHit, ReaderSection, ReaderSettings } from './adapter'
