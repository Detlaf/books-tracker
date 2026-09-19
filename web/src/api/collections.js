import { request } from './client'

export function list() {
  return request('/collections')
}

export function create(name) {
  return request('/collections', { method: 'POST', body: { name } })
}

export function rename(id, name) {
  return request(`/collections/${id}`, { method: 'PATCH', body: { name } })
}

export function remove(id) {
  return request(`/collections/${id}`, { method: 'DELETE' })
}

export function addBook(id, bookId) {
  return request(`/collections/${id}/books`, { method: 'POST', body: { book_id: bookId } })
}

export function removeBook(id, bookId) {
  return request(`/collections/${id}/books/${bookId}`, { method: 'DELETE' })
}
