import { request } from './client'

// The backend caps limit at library.MaxListLimit; the app pages until it has
// everything because the Reports tab is computed from the full library.
export function list({ status, sort, page = 1, limit = 100 } = {}) {
  const params = new URLSearchParams({ page: String(page), limit: String(limit) })
  if (status) params.set('status', status)
  if (sort) params.set('sort', sort)
  return request(`/library?${params}`)
}

export function add(bookId, status, finishedAt) {
  const body = { book_id: bookId, status }
  if (finishedAt !== undefined) body.finished_at = finishedAt
  return request('/library', { method: 'POST', body })
}

// finished_at is sent explicitly as null to clear it — omitting the key means
// "leave unchanged", which is a different request.
export function update(bookId, { status, finishedAt } = {}) {
  const body = {}
  if (status !== undefined) body.status = status
  if (finishedAt !== undefined) body.finished_at = finishedAt
  return request(`/library/${bookId}`, { method: 'PATCH', body })
}

export function remove(bookId) {
  return request(`/library/${bookId}`, { method: 'DELETE' })
}
