import { request } from './client'

// Both endpoints upsert the book server-side and return its local id, which is
// what POST /library needs. Nothing else in the app may invent a book id.
export function search(q, { page = 1, limit = 20 } = {}) {
  const params = new URLSearchParams({ q, page: String(page), limit: String(limit) })
  return request(`/books/search?${params}`)
}

export function byISBN(isbn) {
  return request(`/books/isbn/${encodeURIComponent(isbn)}`)
}
