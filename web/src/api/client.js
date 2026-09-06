// Thin fetch wrapper over the Go API.
//
// Tokens live in localStorage rather than memory so a reload does not log the
// user out. The access token is short-lived (see internal/auth); on a 401 the
// client transparently refreshes once and replays the request. Concurrent 401s
// share a single in-flight refresh — otherwise the first response to land
// rotates the refresh token and the rest fail against a revoked one.

const BASE = import.meta.env.VITE_API_BASE ?? ''

const ACCESS_KEY = 'bookish.access_token'
const REFRESH_KEY = 'bookish.refresh_token'

export class ApiError extends Error {
  constructor(status, message) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

export const tokens = {
  get access() {
    return localStorage.getItem(ACCESS_KEY)
  },
  get refresh() {
    return localStorage.getItem(REFRESH_KEY)
  },
  set({ access_token, refresh_token }) {
    localStorage.setItem(ACCESS_KEY, access_token)
    localStorage.setItem(REFRESH_KEY, refresh_token)
  },
  clear() {
    localStorage.removeItem(ACCESS_KEY)
    localStorage.removeItem(REFRESH_KEY)
  },
}

let refreshInFlight = null

// onAuthLost lets the auth store react to a refresh failure without this
// module importing the store (which would be a cycle).
let onAuthLost = () => {}
export function setAuthLostHandler(fn) {
  onAuthLost = fn
}

async function parseError(res) {
  // respondError writes {"error": "..."}; a proxy or a panic may not.
  try {
    const body = await res.json()
    return body.error || res.statusText
  } catch {
    return res.statusText || `HTTP ${res.status}`
  }
}

async function refreshTokens() {
  const refresh_token = tokens.refresh
  if (!refresh_token) throw new ApiError(401, 'not signed in')

  const res = await fetch(`${BASE}/auth/refresh`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ refresh_token }),
  })
  if (!res.ok) {
    tokens.clear()
    onAuthLost()
    throw new ApiError(res.status, await parseError(res))
  }
  const pair = await res.json()
  tokens.set(pair)
  return pair.access_token
}

async function send(path, { method = 'GET', body, auth = true, accessToken } = {}) {
  const headers = {}
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  const token = accessToken ?? (auth ? tokens.access : null)
  if (token) headers.Authorization = `Bearer ${token}`

  return fetch(`${BASE}${path}`, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
  })
}

export async function request(path, opts = {}) {
  let res = await send(path, opts)

  if (res.status === 401 && opts.auth !== false && tokens.refresh) {
    refreshInFlight = refreshInFlight ?? refreshTokens().finally(() => { refreshInFlight = null })
    const access = await refreshInFlight
    res = await send(path, { ...opts, accessToken: access })
  }

  if (!res.ok) throw new ApiError(res.status, await parseError(res))
  if (res.status === 204) return null
  return res.json()
}
