<script setup>
import { ref, computed } from 'vue'
import { useLibraryStore } from '@/stores/library'
import { useRatingsStore } from '@/stores/ratings'
import { useUiStore } from '@/stores/ui'
import { statusLabel, statusTagClass } from '@/lib/status'
import BookCover from '@/components/BookCover.vue'

const library = useLibraryStore()
const ratings = useRatingsStore()
const ui = useUiStore()

// The prototype opened on "reading" via a configurable prop. Reading is the
// useful default — it is the shortest list and the one that changes daily.
const filter = ref('reading')

const filters = [
  { key: 'all', label: 'All' },
  { key: 'backlog', label: 'To Read' },
  { key: 'reading', label: 'Reading' },
  { key: 'read', label: 'Read' },
]

const shown = computed(() => library.filtered(filter.value))

function starText(bookId) {
  const n = ratings.get(bookId)
  return '★★★★★'.slice(0, n) + '☆☆☆☆☆'.slice(0, 5 - n)
}
</script>

<template>
  <h1 class="page-title">My Library</h1>
  <p class="text-muted page-subtitle">Everything you're reading, have read, or plan to.</p>

  <div class="filters">
    <button
      v-for="f in filters"
      :key="f.key"
      type="button"
      class="pill"
      :class="{ 'is-active': filter === f.key }"
      @click="filter = f.key"
    >
      {{ f.label }} ({{ library.counts[f.key] }})
    </button>
  </div>

  <p v-if="library.error" class="form-error">{{ library.error }}</p>
  <p v-else-if="library.loading && !library.loaded" class="text-muted">Loading your library…</p>

  <div v-else class="book-grid">
    <button
      v-for="entry in shown"
      :key="entry.book.id"
      type="button"
      class="book-card"
      @click="ui.openBook(entry.book.id)"
    >
      <BookCover :book="entry.book" height="150px" font-size="40px" />
      <div>
        <div class="card-title title">{{ entry.book.title }}</div>
        <div class="text-muted author">{{ entry.book.authors.join(', ') || 'Unknown author' }}</div>
      </div>
      <div class="card-foot">
        <span class="tag" :class="statusTagClass(entry.status)">{{ statusLabel(entry.status) }}</span>
        <span v-if="entry.status === 'read' && ratings.get(entry.book.id)" class="stars">
          {{ starText(entry.book.id) }}
        </span>
      </div>
    </button>
  </div>

  <p v-if="library.loaded && shown.length === 0" class="text-muted empty">
    No books in this view yet — add some from Search &amp; Add.
  </p>
</template>

<style scoped>
.filters { display: flex; gap: 8px; margin-bottom: 26px; flex-wrap: wrap; }
.title { font-size: 16px; }
.author { font-size: 12px; margin-top: 2px; }
.card-foot { display: flex; align-items: center; justify-content: space-between; gap: 6px; }
.stars { font-size: 13px; color: var(--color-accent); letter-spacing: 1px; }
.empty { margin-top: 24px; }
</style>
