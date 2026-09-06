// Resolves imported rows against the catalog and writes them to the library.
//
// A spreadsheet row is just text; the API only accepts a book id that came
// from the metadata provider. So each row is resolved first — by ISBN when it
// has one, by a title/author query otherwise — and rows that resolve to
// nothing are reported rather than dropped silently.
//
// Rows are processed one at a time on purpose. Each unresolved row is a
// provider call, and /books/search is rate-limited upstream (the API surfaces
// that as 429); a burst of parallel lookups is the reliable way to trip it.

import * as booksApi from '@/api/books'
import { ApiError } from '@/api/client'

// finished_at goes over the wire as RFC 3339. A spreadsheet usually holds a
// bare YYYY-MM-DD, which is widened to midnight UTC; anything unparseable is
// dropped rather than failing the row, since the status is the more important
// half of the record.
function toRFC3339(value) {
  if (!value) return undefined
  const trimmed = String(value).trim()
  if (/^\d{4}-\d{2}-\d{2}$/.test(trimmed)) return `${trimmed}T00:00:00Z`
  const parsed = new Date(trimmed)
  if (Number.isNaN(parsed.getTime())) return undefined
  return parsed.toISOString().replace(/\.\d{3}Z$/, 'Z')
}

async function resolve(record) {
  if (record.isbn) {
    try {
      return await booksApi.byISBN(record.isbn)
    } catch (e) {
      // Fall through to a text search: a wrong or regional ISBN is common in
      // exports, and the title usually still finds the book.
      if (!(e instanceof ApiError) || (e.status !== 404 && e.status !== 400)) throw e
    }
  }

  const query = [record.title, record.author !== 'Unknown' ? record.author : '']
    .filter(Boolean)
    .join(' ')
  const { items } = await booksApi.search(query, { limit: 1 })
  return items[0] ?? null
}

/**
 * @param records  rows from parseImportFile
 * @param deps     { library, ratings } stores
 * @param onProgress called with (done, total) after each row
 * @returns { added, updated, unmatched, failed }
 */
export async function runImport(records, { library, ratings }, onProgress = () => {}) {
  const result = { added: 0, updated: 0, unmatched: [], failed: [] }

  for (const [i, record] of records.entries()) {
    try {
      const book = await resolve(record)
      if (!book) {
        result.unmatched.push(record.title)
        continue
      }

      const finishedAt = record.status === 'read' ? toRFC3339(record.finishedAt) : undefined
      const existing = library.byBookId.get(book.id)

      if (existing) {
        await library.setStatus(book.id, record.status)
        if (finishedAt && !existing.finished_at) await library.setFinishedAt(book.id, finishedAt)
        result.updated++
      } else {
        await library.add(book.id, record.status, finishedAt)
        result.added++
      }

      // Ratings are local-only until backend M5; an imported one is still
      // worth keeping, and it lands in the same place the star widget reads.
      if (record.rating > 0) ratings.set(book.id, record.rating)
    } catch (e) {
      result.failed.push(`${record.title} (${e.message})`)
    } finally {
      onProgress(i + 1, records.length)
    }
  }

  return result
}
