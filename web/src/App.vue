<script setup>
import { watch } from 'vue'
import { useRoute } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useLibraryStore } from '@/stores/library'
import { useRatingsStore } from '@/stores/ratings'
import { useCollectionsStore } from '@/stores/collections'
import { useSettingsStore } from '@/stores/settings'
import SideNav from '@/components/SideNav.vue'
import BookDetailDialog from '@/components/BookDetailDialog.vue'

const route = useRoute()
const auth = useAuthStore()
const library = useLibraryStore()
const ratings = useRatingsStore()
const collections = useCollectionsStore()
const settings = useSettingsStore()

// Everything user-scoped is (re)loaded when the signed-in user changes, and
// dropped on sign-out. The local stores hydrate after the auth store has set
// the localStorage scope, so account A never reads account B's keys.
watch(
  () => auth.userId,
  (id) => {
    if (id === null) {
      library.reset()
      ratings.reset()
      collections.reset()
      settings.reset()
      return
    }
    ratings.hydrate()
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
