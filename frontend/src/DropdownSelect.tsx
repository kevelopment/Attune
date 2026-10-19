import { useEffect, useId, useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from 'react'
import { createPortal } from 'react-dom'
import { Check, ChevronDown } from 'lucide-react'

export type DropdownOption = { value: string; label: string }

export default function DropdownSelect({ label, value, options, onChange, className = '' }: {
  label: string
  value: string
  options: DropdownOption[]
  onChange: (value: string) => void
  className?: string
}) {
  const id = useId()
  const trigger = useRef<HTMLButtonElement>(null)
  const menu = useRef<HTMLDivElement>(null)
  const [open, setOpen] = useState(false)
  const [active, setActive] = useState(Math.max(0, options.findIndex(option => option.value === value)))
  const [position, setPosition] = useState<{ left: number; top: number; width: number; maxHeight: number } | null>(null)
  const selected = options.find(option => option.value === value) ?? options[0]

  function placeMenu() {
    const anchor = trigger.current?.closest('.sort-select, .filter-select') ?? trigger.current
    const rect = anchor?.getBoundingClientRect()
    if (!rect) return
    const width = Math.min(Math.max(rect.width, 200), window.innerWidth - 16)
    const left = Math.min(Math.max(8, rect.left), window.innerWidth - width - 8)
    const desiredHeight = Math.min(320, Math.max(80, options.length * 38 + 10))
    const belowSpace = Math.max(0, window.innerHeight - rect.bottom - 12)
    const aboveSpace = Math.max(0, rect.top - 12)
    const below = belowSpace >= Math.min(desiredHeight, 180) || belowSpace >= aboveSpace
    const maxHeight = Math.max(80, Math.min(desiredHeight, below ? belowSpace : aboveSpace))
    setPosition({ left, top: below ? rect.bottom + 5 : Math.max(8, rect.top - maxHeight - 5), width, maxHeight })
  }

  function close(restoreFocus = false) {
    setOpen(false)
    if (restoreFocus) trigger.current?.focus()
  }

  function show(index = options.findIndex(option => option.value === value)) {
    setActive(Math.max(0, index))
    placeMenu()
    setOpen(true)
  }

  function choose(index: number) {
    const option = options[index]
    if (!option) return
    onChange(option.value)
    close(true)
  }

  useEffect(() => {
    if (!open) return
    const outside = (event: PointerEvent) => {
      if (!trigger.current?.contains(event.target as Node) && !menu.current?.contains(event.target as Node)) close()
    }
    const reposition = () => placeMenu()
    const escape = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && (event.target as HTMLElement).closest?.('.dropdown-select-trigger')) return
      if (event.key === 'Escape' && open) { event.preventDefault(); event.stopPropagation(); close(true) }
      if (event.key === 'Tab') close()
    }
    document.addEventListener('pointerdown', outside)
    document.addEventListener('keydown', escape)
    window.addEventListener('resize', reposition)
    window.addEventListener('scroll', reposition, true)
    return () => {
      document.removeEventListener('pointerdown', outside)
      document.removeEventListener('keydown', escape)
      window.removeEventListener('resize', reposition)
      window.removeEventListener('scroll', reposition, true)
    }
  }, [open, options.length])

  useEffect(() => {
    if (open) menu.current?.querySelector<HTMLElement>(`[data-index="${active}"]`)?.scrollIntoView({ block: 'nearest' })
  }, [open, active])

  const move = (delta: number) => setActive(index => Math.max(0, Math.min(options.length - 1, index + delta)))
  const onKeyDown = (event: ReactKeyboardEvent<HTMLButtonElement>) => {
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault()
      if (!open) show(Math.max(0, options.findIndex(option => option.value === value)))
      else move(event.key === 'ArrowDown' ? 1 : -1)
    } else if (event.key === 'Home' || event.key === 'End') {
      event.preventDefault()
      const index = event.key === 'Home' ? 0 : options.length - 1
      if (open) setActive(index); else show(index)
    } else if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault()
      if (open) choose(active); else show()
    } else if (event.key === 'Escape' && open) {
      event.preventDefault(); event.stopPropagation(); close(true)
    }
  }

  return <>
    <button ref={trigger} type="button" className={`dropdown-select-trigger ${className}`} role="combobox" aria-label={label} aria-haspopup="listbox" aria-controls={id} aria-expanded={open} aria-activedescendant={open ? `${id}-option-${active}` : undefined} onClick={() => open ? close() : show()} onKeyDown={onKeyDown}>
      <span>{selected?.label ?? ''}</span><ChevronDown size={14} aria-hidden="true"/>
    </button>
    {open && position && createPortal(<div ref={menu} id={id} className="dropdown-select-menu" data-dropdown-select-menu role="listbox" aria-label={label} style={position}>
      {options.map((option, index) => <div key={option.value} id={`${id}-option-${index}`} data-index={index} role="option" aria-selected={option.value === value} className={`dropdown-select-option ${index === active ? 'active' : ''}`} onMouseEnter={() => setActive(index)} onMouseDown={event => event.preventDefault()} onClick={() => choose(index)}>
        <span>{option.label}</span>{option.value === value && <Check size={15} aria-hidden="true"/>}
      </div>)}
    </div>, document.body)}
  </>
}
