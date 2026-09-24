// Pure presentation helpers for the Reports screen. All data shaping now
// happens server-side (backend Milestone 7, /stats/*, internal/stats) — this
// module only turns API-shaped numbers into pixels, percentages and labels.

// barHeight scales count against max onto [min, min + scale], the shared
// visual language of the year and month charts. When zeroStub is set, a
// zero count gets that fixed height instead of collapsing to the minimum —
// the month chart uses this so an empty month still reads as a bar, not a
// gap in the axis.
export function barHeight(count, max, { min = 10, scale = 110, zeroStub = null } = {}) {
  if (zeroStub !== null && count === 0) return zeroStub
  return Math.round((count / Math.max(1, max)) * scale) + min
}

export function barColor(isHighlighted, { on = 'var(--color-accent)', off = 'var(--color-neutral-400)' } = {}) {
  return isHighlighted ? on : off
}

// pct is a bar width as a percentage of the leading count in its group.
export function pct(count, max) {
  return Math.round((count / Math.max(1, max)) * 100)
}

// initial is the avatar letter for an author row.
export function initial(name) {
  return (name?.[0] || '?').toUpperCase()
}

export function yoyLabel(thisYearCount, lastYearCount) {
  const delta = thisYearCount - lastYearCount
  return lastYearCount === 0
    ? `vs ${lastYearCount} last year`
    : `${delta >= 0 ? '+' : ''}${delta} vs last year`
}
