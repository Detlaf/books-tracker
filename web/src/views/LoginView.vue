<script setup>
import { ref, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

// The design shows a sign-in card only, because in the prototype any
// credentials worked. Against the real API an account has to exist first, so
// the same card doubles as registration — roadmap.md Phase 2 lists "Auth pages
// (login / register)" anyway.
const mode = ref('login')
const email = ref('')
const password = ref('')

const isRegister = computed(() => mode.value === 'register')
const heading = computed(() => (isRegister.value ? 'Start your reading log.' : 'Sign in to your reading log.'))
const submitLabel = computed(() => (isRegister.value ? 'Create account' : 'Log in'))

function toggleMode() {
  mode.value = isRegister.value ? 'login' : 'register'
  auth.error = ''
}

async function submit() {
  const ok = isRegister.value
    ? await auth.register(email.value, password.value)
    : await auth.login(email.value, password.value)
  if (ok) router.push(route.query.redirect || { name: 'library' })
}
</script>

<template>
  <div class="login-page">
    <form class="card login-card" @submit.prevent="submit">
      <div class="login-head">
        <div class="brand">Bookish</div>
        <p class="text-muted tagline">{{ heading }}</p>
      </div>

      <div class="field">
        <label for="email">Email</label>
        <input
          id="email"
          v-model="email"
          class="input"
          type="email"
          autocomplete="email"
          placeholder="you@example.com"
          required
        />
      </div>

      <div class="field">
        <label for="password">Password</label>
        <input
          id="password"
          v-model="password"
          class="input"
          type="password"
          :autocomplete="isRegister ? 'new-password' : 'current-password'"
          placeholder="••••••••"
          minlength="8"
          required
        />
      </div>

      <p v-if="auth.error" class="form-error">{{ auth.error }}</p>

      <button class="btn btn-primary btn-block" type="submit" :disabled="auth.pending">
        {{ auth.pending ? 'Working…' : submitLabel }}
      </button>

      <p class="text-muted switch">
        <template v-if="isRegister">Already have an account? </template>
        <template v-else>New here? </template>
        <button class="btn-ghost link" type="button" @click="toggleMode">
          {{ isRegister ? 'Log in' : 'Create one' }}
        </button>
      </p>
      <p v-if="isRegister" class="text-muted hint">Passwords must be at least 8 characters.</p>
    </form>
  </div>
</template>

<style scoped>
.login-page {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--color-bg);
  color: var(--color-text);
  font-family: var(--font-body);
  padding: 24px;
}
.login-card {
  width: min(360px, 100%);
  padding: 32px;
  display: flex;
  flex-direction: column;
  gap: 18px;
}
.login-head { text-align: center; }
.brand { font-family: var(--font-heading); font-weight: 600; font-size: 26px; }
.tagline { margin: 6px 0 0; font-size: 13px; }
.switch { text-align: center; font-size: 12px; margin: 0; }
.hint { text-align: center; font-size: 11px; margin: 0; }
.link {
  border: none;
  background: transparent;
  font-family: var(--font-body);
  font-size: 12px;
  cursor: pointer;
}
</style>
