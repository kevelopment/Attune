import { useEffect, useState } from 'react'

export const textSizes = { small: 12, medium: 14, large: 16 } as const
export type TextSize = keyof typeof textSizes
const storageKey = 'attune.textSize'

export function useTextSize() {
  const [textSize, setTextSize] = useState<TextSize>(() => {
    try {
      const saved = localStorage.getItem(storageKey)
      if (saved === 'small' || saved === 'medium' || saved === 'large') return saved
    } catch { /* Keep the default when local storage is unavailable. */ }
    return 'medium'
  })
  useEffect(() => {
    document.documentElement.style.fontSize = `${textSizes[textSize]}px`
    try { localStorage.setItem(storageKey, textSize) } catch { /* The current choice still works for this session. */ }
  }, [textSize])
  return [textSize, setTextSize] as const
}

export const bookSizeRange = { min: 80, max: 140, step: 10, defaultValue: 100 } as const
const bookSizeStorageKey = 'attune.bookSize'

export function useBookSize() {
  const [bookSize, setBookSize] = useState<number>(() => {
    try {
      const saved = Number(localStorage.getItem(bookSizeStorageKey))
      if (Number.isInteger(saved) && saved >= bookSizeRange.min && saved <= bookSizeRange.max && (saved - bookSizeRange.min) % bookSizeRange.step === 0) return saved
    } catch { /* Keep the default when local storage is unavailable. */ }
    return bookSizeRange.defaultValue
  })
  useEffect(() => {
    try { localStorage.setItem(bookSizeStorageKey, String(bookSize)) } catch { /* The current choice still works for this session. */ }
  }, [bookSize])
  return [bookSize, setBookSize] as const
}
