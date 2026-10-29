import { describe, expect, it } from 'vitest'
import { emacsAction } from './keys'

const key = (code: string, mods: { ctrl?: boolean; alt?: boolean; meta?: boolean; shift?: boolean } = {}) =>
  emacsAction({ code, ctrlKey: !!mods.ctrl, altKey: !!mods.alt, metaKey: !!mods.meta, shiftKey: !!mods.shift })

describe('Emacs keys', () => {
  it('move with Control, and by unread with Meta', () => {
    expect(key('KeyN', { ctrl: true })).toBe('next')
    expect(key('KeyP', { ctrl: true })).toBe('prev')
    expect(key('KeyN', { alt: true })).toBe('nextUnread')
    expect(key('KeyP', { alt: true })).toBe('prevUnread')
  })

  it('page with C-v and M-v, search with C-s, quit with C-g', () => {
    expect(key('KeyV', { ctrl: true })).toBe('pageDown')
    expect(key('KeyV', { alt: true })).toBe('pageUp')
    expect(key('KeyS', { ctrl: true })).toBe('search')
    expect(key('KeyG', { ctrl: true })).toBe('quit')
  })

  it('go to the beginning and end with M-< and M->', () => {
    expect(key('Comma', { alt: true, shift: true })).toBe('top')
    expect(key('Period', { alt: true, shift: true })).toBe('bottom')
    expect(key('IntlBackslash', { alt: true })).toBe('top')
    expect(key('IntlBackslash', { alt: true, shift: true })).toBe('bottom')
    expect(key('Comma', { alt: true })).toBeNull()
  })

  it('leave everything else alone', () => {
    expect(key('KeyN')).toBeNull()
    expect(key('KeyN', { shift: true })).toBeNull()
    expect(key('KeyN', { meta: true })).toBeNull()
    expect(key('KeyN', { ctrl: true, meta: true })).toBeNull()
    expect(key('KeyN', { ctrl: true, alt: true })).toBeNull()
    expect(key('KeyN', { ctrl: true, shift: true })).toBeNull()
    expect(key('KeyS', { alt: true })).toBeNull()
    expect(key('KeyK', { ctrl: true })).toBeNull()
  })
})
