// The design calls the first status "To Read"; the API calls it "backlog"
// (library.ParseStatus accepts backlog / reading / read and 400s on anything
// else). The API vocabulary is what travels over the wire and what is stored,
// so it is what the app uses internally — this module is the only place the
// design's label is attached to it.

export const STATUSES = ['backlog', 'reading', 'read']

const LABELS = {
  backlog: 'To Read',
  reading: 'Reading',
  read: 'Read',
}

const TAG_CLASSES = {
  backlog: 'tag-neutral',
  reading: 'tag-accent-2',
  read: 'tag-accent',
}

export function statusLabel(status) {
  return LABELS[status] ?? ''
}

export function statusTagClass(status) {
  return TAG_CLASSES[status] ?? 'tag-neutral'
}

// Accepts the loose spellings a spreadsheet import might carry, mirroring the
// prototype's normalizeStatus. Anything unrecognized lands in the backlog,
// which is the harmless default: it shows up in the library for the user to
// correct rather than being silently dropped.
export function normalizeStatus(raw) {
  const v = String(raw || '').trim().toLowerCase()
  if (['read', 'finished', 'done'].includes(v)) return 'read'
  if (['reading', 'in progress', 'in-progress', 'current'].includes(v)) return 'reading'
  return 'backlog'
}
