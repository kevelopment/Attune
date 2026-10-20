import type { ReaderAdapter, ReflowableFormat } from '../adapter'
import { FoliateReflowableAdapter } from './foliate'
import { TextReaderAdapter } from './txt'

/** Return an adapter for a supported reflowable format; Foliate loads only when open() is called. */
export function createReflowableAdapter(format: ReflowableFormat): ReaderAdapter {
  return format === 'txt' ? new TextReaderAdapter() : new FoliateReflowableAdapter(format)
}

export { FoliateReflowableAdapter, TextReaderAdapter }
