import { request, tokens } from './client'

export function register(email, password) {
  return request('/auth/register', { method: 'POST', body: { email, password }, auth: false })
}

export async function login(email, password) {
  const pair = await request('/auth/login', {
    method: 'POST',
    body: { email, password },
    auth: false,
  })
  tokens.set(pair)
  return pair
}

// logout revokes the refresh token server-side. A failure here is not worth
// surfacing — the local tokens are cleared either way, so the user is signed
// out of this browser regardless.
export async function logout() {
  const refresh_token = tokens.refresh
  if (refresh_token) {
    try {
      await request('/auth/logout', { method: 'POST', body: { refresh_token } })
    } catch {
      /* ignore */
    }
  }
  tokens.clear()
}

export function me() {
  return request('/me')
}
