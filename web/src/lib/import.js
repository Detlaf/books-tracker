// Reading-history import from .txt / .csv / .xlsx.
//
// Ported from the prototype's parser. SheetJS is a real dependency here rather
// than the prototype's CDN <script>, so the parsed sheet path is identical but
// nothing is fetched at runtime.
//
// Parsing only — this module returns rows, it does not touch the API. The
// settings store decides what to do with them.

import { read as readWorkbook, utils as xlsxUtils } from 'xlsx'
import { normalizeStatus } from './status'

// Header aliases, lowercased. The prototype accepted a handful of spellings
// per column; the same set is kept so an export that worked there still works.
const FIELDS = {
  title: ['title', 'book title'],
  author: ['author', 'authors'],
  status: ['status'],
  rating: ['rating'],
  finishedAt: ['date finished', 'datefinished', 'date'],
  language: ['language'],
  isbn: ['isbn'],
}

function pick(row, aliases) {
  for (const a of aliases) {
    if (row[a] !== undefined && row[a] !== '') return String(row[a]).trim()
  }
  return ''
}

// Splits on tab when the header row contains one, comma otherwise. This is not
// a full CSV parser — it does not handle quoted fields containing the
// delimiter. That matches the prototype, and a title with a comma in it is the
// known limitation; .xlsx is the lossless path.
export function parseDelimitedText(text) {
  const lines = text.split(/\r?\n/).filter((l) => l.trim().length)
  if (!lines.length) return []

  const delim = lines[0].includes('\t') ? '\t' : ','
  const headers = lines[0].split(delim).map((h) => h.trim().toLowerCase())

  return lines.slice(1).map((line) => {
    const cells = line.split(delim)
    const row = {}
    headers.forEach((h, i) => {
      row[h] = (cells[i] || '').trim()
    })
    return row
  })
}

export function parseWorkbook(arrayBuffer) {
  const wb = readWorkbook(arrayBuffer, { type: 'array' })
  const sheet = wb.Sheets[wb.SheetNames[0]]
  if (!sheet) return []
  return xlsxUtils.sheet_to_json(sheet, { defval: '' }).map((r) => {
    const norm = {}
    for (const k of Object.keys(r)) norm[k.trim().toLowerCase()] = String(r[k])
    return norm
  })
}

// Turns a raw header-keyed row into the shape the import flow works with.
// Returns null for a row with no title, which is how blank trailing lines and
// separator rows get dropped.
export function toRecord(row) {
  const title = pick(row, FIELDS.title)
  if (!title) return null

  const ratingRaw = parseInt(pick(row, FIELDS.rating), 10)
  const finishedAt = pick(row, FIELDS.finishedAt)

  return {
    title,
    author: pick(row, FIELDS.author) || 'Unknown',
    status: normalizeStatus(pick(row, FIELDS.status)),
    rating: Number.isNaN(ratingRaw) ? 0 : Math.max(0, Math.min(5, ratingRaw)),
    finishedAt: finishedAt || null,
    language: pick(row, FIELDS.language) || '',
    isbn: pick(row, FIELDS.isbn).replace(/[\s-]/g, ''),
  }
}

function readFile(file, as) {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onerror = () => reject(new Error('Could not read that file.'))
    reader.onload = (e) => resolve(e.target.result)
    if (as === 'buffer') reader.readAsArrayBuffer(file)
    else reader.readAsText(file)
  })
}

export async function parseImportFile(file) {
  const ext = file.name.split('.').pop().toLowerCase()

  let rows
  if (ext === 'xlsx' || ext === 'xls') {
    try {
      rows = parseWorkbook(await readFile(file, 'buffer'))
    } catch {
      throw new Error('Could not read that spreadsheet.')
    }
  } else {
    rows = parseDelimitedText(await readFile(file, 'text'))
  }

  return rows.map(toRecord).filter(Boolean)
}
