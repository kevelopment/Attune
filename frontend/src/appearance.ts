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
