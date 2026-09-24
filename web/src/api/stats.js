import { request } from './client'

export function summary() {
  return request('/stats/summary')
}

export function byYear() {
  return request('/stats/by-year')
}

export function byMonth(year) {
  return request(`/stats/by-month${year ? `?year=${year}` : ''}`)
}

export function byLanguage(scope) {
  return request(`/stats/by-language${scope ? `?scope=${scope}` : ''}`)
}

export function topAuthors(scope, limit) {
  const params = new URLSearchParams()
  if (scope) params.set('scope', scope)
  if (limit) params.set('limit', String(limit))
  const qs = params.toString()
  return request(`/stats/top-authors${qs ? `?${qs}` : ''}`)
}

export function streak() {
  return request('/stats/streak')
}
