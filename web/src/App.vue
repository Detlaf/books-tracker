<script setup>
import { watch } from 'vue'
import { useRoute } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useLibraryStore } from '@/stores/library'
import { useCollectionsStore } from '@/stores/collections'
import { useSettingsStore } from '@/stores/settings'
import { useStatsStore } from '@/stores/stats'
import SideNav from '@/components/SideNav.vue'
import BookDetailDialog from '@/components/BookDetailDialog.vue'

const route = useRoute()
const auth = useAuthStore()
const library = useLibraryStore()
const collections = useCollectionsStore()
const settings = useSettingsStore()
const stats = useStatsStore()

// Everything user-scoped is (re)loaded when the signed-in user changes, and
// dropped on sign-out. Ratings have no independent state to load or clear —
// they read through the library store — so only collections, settings and
// stats hydrate here alongside the library fetch.
watch(
  () => auth.userId,
  (id) => {
    if (id === null) {
      library.reset()
      collections.reset()
      settings.reset()
      stats.reset()
      return
    }
    collections.hydrate()
    settings.hydrate(auth.email.split('@')[0])
    library.fetchAll()
  },
  { immediate: true },
)
</script>

<template>
  <div v-if="auth.isAuthenticated" class="app-shell">
    <SideNav />
    <main class="main-area">
      <RouterView />
    </main>
    <BookDetailDialog />
  </div>

  <!-- The login screen is its own full-page layout, with no nav around it. -->
  <RouterView v-else-if="auth.ready" :key="route.fullPath" />
</template>
