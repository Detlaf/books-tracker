<script setup>
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useLibraryStore } from '@/stores/library'
import { useSettingsStore } from '@/stores/settings'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const library = useLibraryStore()
const settings = useSettingsStore()

// The collection detail route is a child of Collections as far as the nav is
// concerned, so the tab stays lit while a collection is open.
const items = [
  { name: 'library', label: 'Library', match: ['library'] },
  { name: 'search', label: 'Search & Add', match: ['search'] },
  { name: 'reports', label: 'Reports', match: ['reports'] },
  { name: 'collections', label: 'Collections', match: ['collections', 'collection'] },
  { name: 'settings', label: 'Settings', match: ['settings'] },
]

const activeName = computed(() => route.name)
const isActive = (item) => item.match.includes(activeName.value)

const displayName = computed(() => settings.displayName || auth.email || 'Reader')

async function signOut() {
  await auth.logout()
  router.push({ name: 'login' })
}
</script>

<template>
  <nav class="side-nav">
    <div class="brand-label">Bookish</div>

    <RouterLink
      v-for="item in items"
      :key="item.name"
      :to="{ name: item.name }"
      class="nav-item"
      :class="{ 'is-active': isActive(item) }"
    >
      <svg
        width="18"
        height="18"
        viewBox="0 0 24 24"
        fill="none"
        :stroke="isActive(item) ? 'var(--color-accent)' : 'currentColor'"
        stroke-width="2"
        stroke-linecap="round"
        stroke-linejoin="round"
      >
        <template v-if="item.name === 'library'">
          <path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20" />
          <path d="M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2z" />
        </template>
        <template v-else-if="item.name === 'search'">
          <circle cx="11" cy="11" r="8" />
          <path d="m21 21-4.3-4.3" />
        </template>
        <template v-else-if="item.name === 'reports'">
          <line x1="18" y1="20" x2="18" y2="10" />
          <line x1="12" y1="20" x2="12" y2="4" />
          <line x1="6" y1="20" x2="6" y2="14" />
        </template>
        <template v-else-if="item.name === 'collections'">
          <path
            d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"
          />
        </template>
        <template v-else>
          <circle cx="12" cy="12" r="3" />
          <path
            d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"
          />
        </template>
      </svg>
      <span class="nav-label">{{ item.label }}</span>
    </RouterLink>

    <div class="nav-footer">
      <div class="nav-label nav-user">{{ displayName }}</div>
      <div class="nav-label nav-count">{{ library.count }} books tracked</div>
      <button class="nav-label nav-logout" type="button" @click="signOut">Log out</button>
    </div>
  </nav>
</template>

<style scoped>
/* RouterLink renders an <a>; the shared .nav-item class assumes a button. */
.nav-item {
  text-decoration: none;
}
</style>
