// Reading statistics, derived on the client.
//
// Backend Milestone 7 (/stats/*) is not built yet, but every figure the
// Reports screen shows is already recoverable from GET /library, which returns
// status, finished_at, language and authors for the whole library. So this
// module computes them here. When M7 lands these functions become the shape to
// check the endpoints against, and the screen can switch over one card at a
// time.
//
// It follows M7's stated rule on undated books (specs/backend/implementation.md):
// rows with status=read but no finished_at count toward the summary, language
// and author reports, but cannot be placed in a year — so they are excluded
// from the by-year and by-month charts and reported as a separate count, which
// is what lets the totals reconcile.

export const MONTH_ABBREVS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']

const yearOf = (entry) => entry.finished_at?.slice(0, 4) ?? null
const monthOf = (entry) => (entry.finished_at ? parseInt(entry.finished_at.slice(5, 7), 10) : null)

export function readEntries(entries) {
  return entries.filter((e) => e.status === 'read')
}

export function datedReadEntries(entries) {
  return readEntries(entries).filter((e) => e.finished_at)
}

export function summary(entries, now = new Date()) {
  const read = readEntries(entries)
  const dated = read.filter((e) => e.finished_at)
  const currentYear = now.getFullYear()
  const countInYear = (y) => dated.filter((e) => yearOf(e) === String(y)).length

  const thisYearCount = countInYear(currentYear)
  const lastYearCount = countInYear(currentYear - 1)
  const delta = thisYearCount - lastYearCount

  return {
    totalRead: read.length,
    undatedRead: read.length - dated.length,
    thisYearCount,
    lastYearCount,
    currentYear,
    yoyLabel:
      lastYearCount === 0
        ? `vs ${lastYearCount} last year`
        : `${delta >= 0 ? '+' : ''}${delta} vs last year`,
  }
}

export function byYear(entries, now = new Date()) {
  const dated = datedReadEntries(entries)
  const currentYear = String(now.getFullYear())

  const counts = new Map()
  for (const e of dated) {
    const y = yearOf(e)
    counts.set(y, (counts.get(y) ?? 0) + 1)
  }

  const years = [...counts.keys()].sort()
  const max = Math.max(1, ...counts.values())

  return years.map((year) => {
    const count = counts.get(year)
    return {
      year,
      count,
      barHeight: Math.round((count / max) * 110) + 10,
      barColor: year === currentYear ? 'var(--color-accent)' : 'var(--color-neutral-400)',
    }
  })
}

export function byMonth(entries, year, now = new Date()) {
  const target = String(year ?? now.getFullYear())
  const dated = datedReadEntries(entries).filter((e) => yearOf(e) === target)

  const counts = MONTH_ABBREVS.map(
    (_, i) => dated.filter((e) => monthOf(e) === i + 1).length,
  )
  const max = Math.max(1, ...counts)

  return MONTH_ABBREVS.map((label, i) => ({
    label,
    count: counts[i],
    // Zero months keep a 2px stub so the axis reads as a continuous 12 months
    // rather than a row with holes in it.
    barHeight: counts[i] === 0 ? 2 : Math.round((counts[i] / max) * 70) + 6,
    barColor: counts[i] > 0 ? 'var(--color-accent-500)' : 'var(--color-neutral-200)',
  }))
}

// Consecutive months, counting back from the last completed month, in which at
// least one book was finished. The month in progress is skipped: it is not
// over yet, so a zero there is not a broken streak.
export function currentStreak(entries, now = new Date()) {
  const dated = datedReadEntries(entries)
  if (dated.length === 0) return 0

  const finished = new Set(dated.map((e) => `${yearOf(e)}-${String(monthOf(e)).padStart(2, '0')}`))

  let y = now.getFullYear()
  let m = now.getMonth() + 1
  let streak = 0
  // 240 months is a bound, not a limit anyone reaches; it keeps a data bug
  // from spinning here forever.
  for (let i = 0; i < 240; i++) {
    m -= 1
    if (m === 0) {
      m = 12
      y -= 1
    }
    if (finished.has(`${y}-${String(m).padStart(2, '0')}`)) streak++
    else break
  }
  return streak
}

function tally(entries, key) {
  const counts = new Map()
  for (const e of entries) {
    for (const value of key(e)) {
      if (!value) continue
      counts.set(value, (counts.get(value) ?? 0) + 1)
    }
  }
  return counts
}

// scope is a four-digit year string, or 'all'.
export function scopedRead(entries, scope) {
  const read = readEntries(entries)
  if (scope === 'all') return read
  return read.filter((e) => yearOf(e) === scope)
}

export function byLanguage(entries, scope = 'all') {
  const counts = tally(scopedRead(entries, scope), (e) => [e.book.language || 'Unknown'])
  const max = Math.max(1, ...counts.values())
  return [...counts.entries()]
    .sort((a, b) => b[1] - a[1])
    .map(([language, count]) => ({ language, count, pct: Math.round((count / max) * 100) }))
}

// A book with several authors counts once for each of them, so the ranking
// answers "whose writing have I read most", not "whose name is listed first".
export function topAuthors(entries, scope = 'all', limit = 5) {
  const counts = tally(scopedRead(entries, scope), (e) => e.book.authors ?? [])
  return [...counts.entries()]
    .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
    .slice(0, limit)
    .map(([author, count]) => ({ author, count, initial: (author[0] || '?').toUpperCase() }))
}

export function availableYears(entries) {
  return [...new Set(datedReadEntries(entries).map(yearOf))].sort().reverse()
}
