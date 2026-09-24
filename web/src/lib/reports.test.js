import { describe, it, expect } from 'vitest'
import { barHeight, barColor, pct, initial, yoyLabel } from './reports'

describe('barHeight', () => {
  it('scales the leader to min + scale', () => {
    expect(barHeight(10, 10, { min: 10, scale: 110 })).toBe(120)
  })

  it('scales a smaller count proportionally', () => {
    expect(barHeight(5, 10, { min: 10, scale: 110 })).toBe(65)
  })

  it('treats a zero max as one, so an empty group does not divide by zero', () => {
    expect(barHeight(0, 0, { min: 10, scale: 110 })).toBe(10)
  })

  it('uses the zero stub instead of the scaled minimum when given one', () => {
    expect(barHeight(0, 5, { min: 6, scale: 70, zeroStub: 2 })).toBe(2)
  })
})

describe('barColor', () => {
  it('returns the "on" color when highlighted', () => {
    expect(barColor(true, { on: 'accent', off: 'neutral' })).toBe('accent')
  })

  it('returns the "off" color otherwise', () => {
    expect(barColor(false, { on: 'accent', off: 'neutral' })).toBe('neutral')
  })

  it('defaults to the design system tokens', () => {
    expect(barColor(true)).toBe('var(--color-accent)')
    expect(barColor(false)).toBe('var(--color-neutral-400)')
  })
})

describe('pct', () => {
  it('scales against the leader', () => {
    expect(pct(2, 2)).toBe(100)
    expect(pct(1, 2)).toBe(50)
  })

  it('treats a zero max as one', () => {
    expect(pct(0, 0)).toBe(0)
  })
})

describe('initial', () => {
  it('uppercases the first letter', () => {
    expect(initial('ann leckie')).toBe('A')
  })

  it('falls back to a question mark for an empty name', () => {
    expect(initial('')).toBe('?')
  })
})

describe('yoyLabel', () => {
  it('shows a signed delta when last year had reads', () => {
    expect(yoyLabel(5, 4)).toBe('+1 vs last year')
    expect(yoyLabel(3, 4)).toBe('-1 vs last year')
  })

  it('phrases the first year without a delta', () => {
    expect(yoyLabel(1, 0)).toBe('vs 0 last year')
  })
})
