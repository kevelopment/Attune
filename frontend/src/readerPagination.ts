export type ReaderPagination = {
  doc: Document
  width: number
  index: number
  count: number
  position(): number
  goTo(index: number, smooth?: boolean): void
}

export function createReaderPagination(doc: Document, columns: 1 | 2, padding: number, position = 0): ReaderPagination {
  const body = doc.body
  if (!body) throw new Error('Reader document has no body')

  const width = Math.max(1, doc.documentElement.clientWidth)
  const stride = width / columns

  // Remove any old offset before measuring the multicolumn content extent.
  body.style.setProperty('transform', 'none', 'important')
  body.style.setProperty('transition', 'none', 'important')
  body.style.setProperty('transform-origin', 'top left', 'important')
  doc.scrollingElement?.scrollTo(0, 0)
  const columnCount = Math.max(columns, Math.ceil((body.scrollWidth - padding - 1) / stride))
  const count = Math.ceil(columnCount / columns)
  let index = 0

  const pagination: ReaderPagination = {
    doc,
    width,
    get index() { return index },
    count,
    position: () => count <= 1 ? 0 : index / (count - 1),
    goTo(nextIndex, smooth = true) {
      index = Math.max(0, Math.min(count - 1, Math.round(nextIndex)))
      const reduceMotion = doc.defaultView?.matchMedia('(prefers-reduced-motion: reduce)').matches ?? false
      body.style.setProperty('transition', smooth && !reduceMotion ? 'transform 240ms ease' : 'none', 'important')
      body.style.setProperty('transform', `translateX(${-index * width}px)`, 'important')
      body.dataset.readerPageIndex = String(index)
      body.dataset.readerPageCount = String(count)
    },
  }

  pagination.goTo(Math.round(Math.max(0, Math.min(1, position)) * (count - 1)), false)
  return pagination
}
