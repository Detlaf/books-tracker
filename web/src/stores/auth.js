import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import * as authApi from '@/api/auth'
import { tokens, setAuthLostHandler } from '@/api/client'
import { setScope } from '@/lib/localStore'

export const useAuthStore = defineStore('auth', () => {
  const userId = ref(null)
  const email = ref('')
  const ready = ref(false)
  const error = ref('')
  const pending = ref(false)

  const isAuthenticated = computed(() => userId.value !== null)

  // The storage scope is set before userId changes, so the watcher in App.vue
  // that hydrates the local stores cannot read the previous account's keys.
  function adopt(id, addr) {
    setScope(id)
    email.value = addr ?? email.value
    userId.value = id
  }

  function forget() {
    userId.value = null
    email.value = ''
    setScope(null)
  }

  // Called from the API client when a refresh fails, i.e. the session is gone
  // for good. Router guards see isAuthenticated flip and bounce to /login.
  setAuthLostHandler(forget)

  // restore runs once at startup. A stored token may be expired or revoked;
  // /me is the cheapest way to find out, and the client's refresh-and-replay
  // means a merely-expired access token resolves silently here.
  async function restore() {
    if (!tokens.access) {
      ready.value = true
      return
    }
    try {
      const { user_id } = await authApi.me()
      adopt(user_id, localStorage.getItem('bookish.email') || '')
    } catch {
      tokens.clear()
      forget()
    } finally {
      ready.value = true
    }
  }

  async function login(addr, password) {
    pending.value = true
    error.value = ''
    try {
      await authApi.login(addr, password)
      const { user_id } = await authApi.me()
      // The API has no profile endpoint, so the address the user just signed
      // in with is the only name the app has for them.
      localStorage.setItem('bookish.email', addr)
      adopt(user_id, addr)
      return true
    } catch (e) {
      error.value = e.message || 'Could not sign in.'
      return false
    } finally {
      pending.value = false
    }
  }

  async function register(addr, password) {
    pending.value = true
    error.value = ''
    try {
      await authApi.register(addr, password)
    } catch (e) {
      error.value = e.message || 'Could not create that account.'
      pending.value = false
      return false
    }
    pending.value = false
    return login(addr, password)
  }

  async function logout() {
    await authApi.logout()
    localStorage.removeItem('bookish.email')
    forget()
  }

  return { userId, email, ready, error, pending, isAuthenticated, restore, login, register, logout }
})
