import { describe, it, expect, vi, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useStatsStore } from './stats'
import * as statsApi from '@/api/stats'

vi.mock('@/api/stats')

function stubAll() {
  statsApi.summary.mockResolvedValue({
    total_read: 10, reading: 2, backlog: 3,
    this_year: 4, last_year: 5, current_year: 2026, undated_read: 1,
  })
  statsApi.byYear.mockResolvedValue({ years: [{ year: '2025', count: 4 }], undated: 1 })
  statsApi.byMonth.mockResolvedValue({
    year: '2026',
    months: Array.from({ length: 12 }, (_, i) => ({ month: i + 1, count: 0 })),
  })
  statsApi.byLanguage.mockResolvedValue({ languages: [{ language: 'English', count: 4 }] })
  statsApi.topAuthors.mockResolvedValue({ authors: [{ author: 'Ann Leckie', count: 2 }] })
  statsApi.streak.mockResolvedValue({ months: 3 })
}

beforeEach(() => {
  setActivePinia(createPinia())
  vi.resetAllMocks()
})

describe('load', () => {
  it('fetches all six endpoints and normalizes summary to camelCase', async () => {
    stubAll()
    const store = useStatsStore()

    await store.load()

    expect(store.summary).toEqual({
      totalRead: 10, reading: 2, backlog: 3,
      thisYearCount: 4, lastYearCount: 5, currentYear: 2026, undatedRead: 1,
    })
    expect(store.byYear).toEqual({ years: [{ year: '2025', count: 4 }], undated: 1 })
    expect(store.byMonth.year).toBe('2026')
    expect(store.byLanguage).toEqual([{ language: 'English', count: 4 }])
    expect(store.topAuthors).toEqual([{ author: 'Ann Leckie', count: 2 }])
    expect(store.streak).toBe(3)
    expect(store.loaded).toBe(true)
    expect(store.loading).toBe(false)
  })

  it('fetches the scoped endpoints with no scope param on the default "all" scope', async () => {
    stubAll()
    const store = useStatsStore()

    await store.load()

    expect(statsApi.byLanguage).toHaveBeenCalledWith(undefined)
    expect(statsApi.topAuthors).toHaveBeenCalledWith(undefined)
  })

  it('records an error message on failure', async () => {
    statsApi.summary.mockRejectedValue(new Error('boom'))
    statsApi.byYear.mockResolvedValue({ years: [], undated: 0 })
    statsApi.byMonth.mockResolvedValue({ year: '2026', months: [] })
    statsApi.streak.mockResolvedValue({ months: 0 })
    const store = useStatsStore()

    await store.load()

    expect(store.error).toBe('boom')
    expect(store.loading).toBe(false)
  })
})

describe('setScope', () => {
  it('only re-fetches by-language and top-authors', async () => {
    stubAll()
    const store = useStatsStore()
    await store.load()
    vi.clearAllMocks()
    statsApi.byLanguage.mockResolvedValue({ languages: [] })
    statsApi.topAuthors.mockResolvedValue({ authors: [] })

    await store.setScope('2025')

    expect(store.scope).toBe('2025')
    expect(statsApi.byLanguage).toHaveBeenCalledWith('2025')
    expect(statsApi.topAuthors).toHaveBeenCalledWith('2025')
    expect(statsApi.summary).not.toHaveBeenCalled()
    expect(statsApi.byYear).not.toHaveBeenCalled()
    expect(statsApi.byMonth).not.toHaveBeenCalled()
  })
})

describe('setMonthYear', () => {
  it('only re-fetches by-month', async () => {
    stubAll()
    const store = useStatsStore()
    await store.load()
    vi.clearAllMocks()
    statsApi.byMonth.mockResolvedValue({ year: '2024', months: [] })

    await store.setMonthYear('2024')

    expect(store.byMonth).toEqual({ year: '2024', months: [] })
    expect(statsApi.byMonth).toHaveBeenCalledWith('2024')
    expect(statsApi.summary).not.toHaveBeenCalled()
  })
})

describe('reset', () => {
  it('clears all state back to defaults', async () => {
    stubAll()
    const store = useStatsStore()
    await store.load()

    store.reset()

    expect(store.summary).toBeNull()
    expect(store.byYear).toEqual({ years: [], undated: 0 })
    expect(store.byLanguage).toEqual([])
    expect(store.topAuthors).toEqual([])
    expect(store.streak).toBe(0)
    expect(store.scope).toBe('all')
    expect(store.loaded).toBe(false)
  })
})
