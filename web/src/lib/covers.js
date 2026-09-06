// Cover placeholders.
//
// The prototype picked its palette entry by position in a fixed catalog. Real
// books arrive from search in arbitrary order, so the pair is chosen by a hash
// of the book id instead: the same book keeps the same colors across reloads
// and across the grid / dialog / collection thumbnail, which is the property
// that actually mattered.

const PALETTE = [
  ['var(--color-accent-200)', 'var(--color-accent-800)'],
  ['var(--color-accent-2-200)', 'var(--color-accent-2-800)'],
  ['var(--color-neutral-300)', 'var(--color-neutral-800)'],
  ['var(--color-accent-100)', 'var(--color-accent-700)'],
  ['var(--color-neutral-200)', 'var(--color-neutral-700)'],
]

function hash(str) {
  let h = 0
  for (let i = 0; i < str.length; i++) {
    h = (h * 31 + str.charCodeAt(i)) | 0
  }
  return Math.abs(h)
}

export function coverColors(key) {
  return PALETTE[hash(String(key)) % PALETTE.length]
}

// Leading articles are stripped so a shelf of "The …" titles does not become a
// wall of T.
export function coverInitial(title) {
  const stripped = String(title || '?').replace(/^(The |A |An )/, '')
  return (stripped[0] || '?').toUpperCase()
}

// The style object every cover tile shares; size is set by the caller's class.
export function coverStyle(key) {
  const [bg, fg] = coverColors(key)
  return { background: bg, color: fg }
}
