/** What an Emacs-style key does, with the Emacs keys setting on. */
export type EmacsAction = 'next' | 'prev' | 'nextUnread' | 'prevUnread' | 'pageDown' | 'pageUp' | 'top' | 'bottom' | 'search' | 'quit'

type Key = Pick<KeyboardEvent, 'code' | 'ctrlKey' | 'altKey' | 'metaKey' | 'shiftKey'>

const control: Record<string, EmacsAction> = { KeyN: 'next', KeyP: 'prev', KeyV: 'pageDown', KeyS: 'search', KeyG: 'quit' }
const meta: Record<string, EmacsAction> = { KeyN: 'nextUnread', KeyP: 'prevUnread', KeyV: 'pageUp' }

/**
 * Control is Control and Meta is Option (Alt). Keys go by e.code, the key's
 * place, since Option turns e.key into ˜ or π on a Mac. M-< and M-> are
 * Option-Shift-Comma and -Period, or the < > key ISO keyboards have by the
 * left Shift. Command combinations stay the browser's.
 */
export function emacsAction(e: Key): EmacsAction | null {
  if (e.metaKey || e.ctrlKey === e.altKey) return null
  if (e.ctrlKey) return e.shiftKey ? null : (control[e.code] ?? null)
  if (e.code === 'Comma' && e.shiftKey) return 'top'
  if (e.code === 'Period' && e.shiftKey) return 'bottom'
  if (e.code === 'IntlBackslash') return e.shiftKey ? 'bottom' : 'top'
  return e.shiftKey ? null : (meta[e.code] ?? null)
}
