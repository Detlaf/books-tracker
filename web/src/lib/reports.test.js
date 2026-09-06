import { describe, it, expect } from 'vitest'
import {
  summary,
  byYear,
  byMonth,
  currentStreak,
  byLanguage,
  topAuthors,
  availableYears,
} from './reports'

// Matches the GET /library wire shape: an entry wraps a book plus the user's
// status and finish date.
function entry(status, finishedAt, { language = 'en', authors = ['A. Author'], id = 1 } = {}) {
  return {
    book: { id, title: `Book ${id}`, authors, language },
    status,
    finished_at: finishedAt,
    added_at: '2026-01-01T00:00:00Z',
  }
}

const NOW = new Date('2026-08-02T12:00:00Z')

describe('summary', () => {
  it('counts every read book, dated or not', () => {
    const entries = [
      entry('read', '2026-03-01T00:00:00Z', { id: 1 }),
      entry('read', null, { id: 2 }),
      entry('reading', null, { id: 3 }),
      entry('backlog', null, { id: 4 }),
    ]
    const s = summary(entries, NOW)
    expect(s.totalRead).toBe(2)
    expect(s.undatedRead).toBe(1)
    expect(s.thisYearCount).toBe(1)
  })

  it('reports year-over-year against last year', () => {
    const entries = [
      entry('read', '2026-01-01T00:00:00Z', { id: 1 }),
      entry('read', '2026-02-01T00:00:00Z', { id: 2 }),
      entry('read', '2025-05-01T00:00:00Z', { id: 3 }),
    ]
    expect(summary(entries, NOW).yoyLabel).toBe('+1 vs last year')
  })

  it('phrases the first year without a comparison', () => {
    const entries = [entry('read', '2026-01-01T00:00:00Z', { id: 1 })]
    expect(summary(entries, NOW).yoyLabel).toBe('vs 0 last year')
  })
})

describe('byYear', () => {
  it('buckets dated reads and highlights the current year', () => {
    const entries = [
      entry('read', '2025-01-01T00:00:00Z', { id: 1 }),
      entry('read', '2025-06-01T00:00:00Z', { id: 2 }),
      entry('read', '2026-01-01T00:00:00Z', { id: 3 }),
      entry('read', null, { id: 4 }),
    ]
    const buckets = byYear(entries, NOW)
    expect(buckets.map((b) => [b.year, b.count])).toEqual([
      ['2025', 2],
      ['2026', 1],
    ])
    expect(buckets[1].barColor).toBe('var(--color-accent)')
    expect(buckets[0].barColor).toBe('var(--color-neutral-400)')
  })

  it('excludes read books with no finish date', () => {
    expect(byYear([entry('read', null)], NOW)).toEqual([])
  })
})

describe('byMonth', () => {
  it('always returns twelve months', () => {
    expect(byMonth([entry('read', '2026-03-04T00:00:00Z')], 2026, NOW)).toHaveLength(12)
  })

  it('places a finish in the right month', () => {
    const months = byMonth([entry('read', '2026-03-04T00:00:00Z')], 2026, NOW)
    expect(months[2].count).toBe(1)
    expect(months[0].count).toBe(0)
    // Empty months keep a visible stub rather than collapsing to nothing.
    expect(months[0].barHeight).toBe(2)
  })
})

describe('currentStreak', () => {
  it('counts back from the last completed month', () => {
    // now is August; July, June and May each have a finish, April does not.
    const entries = [
      entry('read', '2026-07-10T00:00:00Z', { id: 1 }),
      entry('read', '2026-06-10T00:00:00Z', { id: 2 }),
      entry('read', '2026-05-10T00:00:00Z', { id: 3 }),
      entry('read', '2026-03-10T00:00:00Z', { id: 4 }),
    ]
    expect(currentStreak(entries, NOW)).toBe(3)
  })

  it('ignores the month in progress', () => {
    // A finish this month does not extend a streak that is already broken:
    // July is empty, so the streak is zero.
    expect(currentStreak([entry('read', '2026-08-01T00:00:00Z')], NOW)).toBe(0)
  })

  it('crosses a year boundary', () => {
    const jan = new Date('2026-01-15T00:00:00Z')
    const entries = [
      entry('read', '2025-12-10T00:00:00Z', { id: 1 }),
      entry('read', '2025-11-10T00:00:00Z', { id: 2 }),
    ]
    expect(currentStreak(entries, jan)).toBe(2)
  })

  it('is zero with nothing finished', () => {
    expect(currentStreak([entry('backlog', null)], NOW)).toBe(0)
  })
})

describe('byLanguage', () => {
  it('ranks languages and scales bars against the leader', () => {
    const entries = [
      entry('read', '2026-01-01T00:00:00Z', { id: 1, language: 'en' }),
      entry('read', '2026-02-01T00:00:00Z', { id: 2, language: 'en' }),
      entry('read', '2026-03-01T00:00:00Z', { id: 3, language: 'ja' }),
    ]
    expect(byLanguage(entries)).toEqual([
      { language: 'en', count: 2, pct: 100 },
      { language: 'ja', count: 1, pct: 50 },
    ])
  })

  it('scopes to a single year when asked', () => {
    const entries = [
      entry('read', '2026-01-01T00:00:00Z', { id: 1, language: 'en' }),
      entry('read', '2025-01-01T00:00:00Z', { id: 2, language: 'ja' }),
    ]
    expect(byLanguage(entries, '2025')).toEqual([{ language: 'ja', count: 1, pct: 100 }])
  })

  it('counts undated reads, which the year charts cannot show', () => {
    expect(byLanguage([entry('read', null, { language: 'fr' })])).toEqual([
      { language: 'fr', count: 1, pct: 100 },
    ])
  })
})

describe('topAuthors', () => {
  it('credits every author of a co-written book', () => {
    const entries = [
      entry('read', '2026-01-01T00:00:00Z', { id: 1, authors: ['Ann', 'Bob'] }),
      entry('read', '2026-02-01T00:00:00Z', { id: 2, authors: ['Ann'] }),
    ]
    expect(topAuthors(entries)).toEqual([
      { author: 'Ann', count: 2, initial: 'A' },
      { author: 'Bob', count: 1, initial: 'B' },
    ])
  })

  it('caps the ranking', () => {
    const entries = Array.from({ length: 8 }, (_, i) =>
      entry('read', '2026-01-01T00:00:00Z', { id: i, authors: [`Author ${i}`] }),
    )
    expect(topAuthors(entries)).toHaveLength(5)
  })
})

describe('availableYears', () => {
  it('lists distinct years, newest first', () => {
    const entries = [
      entry('read', '2024-01-01T00:00:00Z', { id: 1 }),
      entry('read', '2026-01-01T00:00:00Z', { id: 2 }),
      entry('read', '2026-05-01T00:00:00Z', { id: 3 }),
    ]
    expect(availableYears(entries)).toEqual(['2026', '2024'])
  })
})
