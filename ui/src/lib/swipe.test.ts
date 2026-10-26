import { describe, expect, it, vi } from 'vitest'
import { dampen, lockAxis, resolveSwipe, swipe } from './swipe'

describe('lockAxis', () => {
  it('waits until the finger has moved', () => {
    expect(lockAxis(3, 4)).toBeNull()
  })
  it('only treats clearly horizontal motion as a swipe', () => {
    expect(lockAxis(20, 2)).toBe('x')
    expect(lockAxis(-20, 5)).toBe('x')
    expect(lockAxis(12, 10)).toBe('y') // diagonal: leave it to scrolling
    expect(lockAxis(2, 30)).toBe('y')
  })
})

describe('resolveSwipe', () => {
  it('commits right for read and left for star past the threshold', () => {
    expect(resolveSwipe(120, 390)).toBe('read')
    expect(resolveSwipe(-120, 390)).toBe('star')
  })
  it('does nothing for short drags', () => {
    expect(resolveSwipe(60, 390)).toBeNull()
    expect(resolveSwipe(-60, 390)).toBeNull()
  })
  it('keeps a minimum threshold on narrow rows', () => {
    expect(resolveSwipe(60, 100)).toBeNull()
    expect(resolveSwipe(70, 100)).toBe('read')
  })
})

describe('dampen', () => {
  it('follows the finger up to the limit, then resists', () => {
    expect(dampen(50, 390)).toBe(50)
    const far = dampen(400, 390)
    expect(far).toBeLessThan(400)
    expect(far).toBeGreaterThan(150)
    expect(dampen(-400, 390)).toBe(-far)
  })
})

describe('swipe action', () => {
  function pointer(type: string, x: number, y: number, pointerType = 'touch') {
    return new PointerEvent(type, { clientX: x, clientY: y, pointerId: 1, pointerType, isPrimary: true, bubbles: true })
  }
  function setup() {
    const node = document.createElement('li')
    Object.defineProperty(node, 'clientWidth', { value: 390 })
    document.body.append(node)
    const onCommit = vi.fn()
    const action = swipe(node, { onCommit })
    return { node, onCommit, action }
  }

  it('commits a long right swipe and suppresses the tap', () => {
    const { node, onCommit } = setup()
    const tapped = vi.fn()
    node.addEventListener('click', tapped)
    node.dispatchEvent(pointer('pointerdown', 10, 100))
    node.dispatchEvent(pointer('pointermove', 60, 102))
    expect(node.style.getPropertyValue('--swipe-dx')).toBe('50px')
    node.dispatchEvent(pointer('pointermove', 160, 104))
    expect(node.dataset.swipe).toBe('read')
    node.dispatchEvent(pointer('pointerup', 160, 104))
    expect(onCommit).toHaveBeenCalledWith('read')
    expect(node.dataset.swipe).toBe('')
    node.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    expect(tapped).not.toHaveBeenCalled()
  })

  it('ignores vertical scrolls and mouse drags', () => {
    const { node, onCommit } = setup()
    node.dispatchEvent(pointer('pointerdown', 10, 100))
    node.dispatchEvent(pointer('pointermove', 14, 160))
    node.dispatchEvent(pointer('pointermove', 200, 170))
    node.dispatchEvent(pointer('pointerup', 200, 170))
    node.dispatchEvent(pointer('pointerdown', 10, 100, 'mouse'))
    node.dispatchEvent(pointer('pointermove', 200, 100, 'mouse'))
    node.dispatchEvent(pointer('pointerup', 200, 100, 'mouse'))
    expect(onCommit).not.toHaveBeenCalled()
  })

  it('cancels cleanly', () => {
    const { node, onCommit, action } = setup()
    node.dispatchEvent(pointer('pointerdown', 10, 100))
    node.dispatchEvent(pointer('pointermove', 200, 100))
    node.dispatchEvent(pointer('pointercancel', 200, 100))
    expect(node.style.getPropertyValue('--swipe-dx')).toBe('')
    expect(onCommit).not.toHaveBeenCalled()
    action.destroy()
  })
})
