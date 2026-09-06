<script setup>
import { computed, ref, watch } from 'vue'
import { useUiStore } from '@/stores/ui'
import { useLibraryStore } from '@/stores/library'
import { useRatingsStore } from '@/stores/ratings'
import { useCollectionsStore } from '@/stores/collections'
import { STATUSES, statusLabel } from '@/lib/status'
import BookCover from '@/components/BookCover.vue'

const ui = useUiStore()
const library = useLibraryStore()
const ratings = useRatingsStore()
const collections = useCollectionsStore()

const error = ref('')
const busy = ref(false)

const entry = computed(() =>
  ui.selectedBookId === null ? null : (library.byBookId.get(ui.selectedBookId) ?? null),
)
const book = computed(() => entry.value?.book ?? null)

const authors = computed(() => book.value?.authors?.join(', ') || 'Unknown author')

// The metadata line only shows the fields the provider actually returned, so a
// book with no language does not render a stray separator.
const metaParts = computed(() => {
  if (!book.value) return []
  const parts = []
  if (book.value.language) parts.push(book.value.language.toUpperCase())
  if (book.value.isbn) parts.push(`ISBN ${book.value.isbn}`)
  return parts
})

const finishedOn = computed(() => {
  const raw = entry.value?.finished_at
  if (!raw) return null
  return new Date(raw).toLocaleDateString(undefined, {
    year: 'numeric',
    month: 'long',
    day: 'numeric',
  })
})

watch(() => ui.selectedBookId, () => { error.value = '' })

async function setStatus(status) {
  if (!entry.value || entry.value.status === status) return
  busy.value = true
  error.value = ''
  try {
    await library.setStatus(entry.value.book.id, status)
  } catch (e) {
    error.value = e.message || 'Could not update that book.'
  } finally {
    busy.value = false
  }
}

const rating = computed(() => (book.value ? ratings.get(book.value.id) : 0))

// The rating widget is only meaningful on a finished book — the backend spec
// for M5 rejects a rating unless status is read, so the UI enforces the same
// rule now rather than letting users set something the API will later refuse.
const canRate = computed(() => entry.value?.status === 'read')

// Clicking the star that is already lit clears the rating — the widget's only
// way to say "no rating" without a separate control.
function rate(n) {
  ratings.set(book.value.id, rating.value === n ? 0 : n)
}

async function removeFromLibrary() {
  if (!entry.value) return
  const bookId = entry.value.book.id
  busy.value = true
  try {
    await library.remove(bookId)
    collections.forgetBook(bookId)
    ratings.clear(bookId)
    ui.closeBook()
  } catch (e) {
    error.value = e.message || 'Could not remove that book.'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div v-if="entry" class="dialog-backdrop" @click="ui.closeBook()">
    <div class="dialog book-dialog" @click.stop>
      <div class="dialog-head">
        <BookCover :book="book" width="70px" height="96px" font-size="28px" />
        <div class="dialog-head-text">
          <div class="dialog-title">{{ book.title }}</div>
          <div class="text-muted author">{{ authors }}</div>
          <div v-if="metaParts.length" class="text-muted meta">{{ metaParts.join(' · ') }}</div>
          <div v-if="finishedOn" class="text-muted meta">Finished {{ finishedOn }}</div>
        </div>
      </div>

      <div>
        <div class="field-label">Status</div>
        <div class="status-row">
          <button
            v-for="status in STATUSES"
            :key="status"
            type="button"
            class="status-btn"
            :class="{ 'is-active': entry.status === status }"
            :disabled="busy"
            @click="setStatus(status)"
          >
            {{ statusLabel(status) }}
          </button>
        </div>
      </div>

      <div>
        <div class="field-label">Your rating</div>
        <div class="stars">
          <button
            v-for="n in 5"
            :key="n"
            type="button"
            class="star"
            :class="{ 'is-on': n <= rating }"
            :disabled="!canRate"
            :aria-label="`${n} star${n > 1 ? 's' : ''}`"
            @click="rate(n)"
          >
            &starf;
          </button>
        </div>
        <p v-if="!canRate" class="stranded-note">Mark this book as read to rate it.</p>
        <p v-else class="stranded-note">Saved in this browser only — backend Milestone 5.</p>
      </div>

      <div>
        <div class="field-label">Collections</div>
        <div v-if="collections.items.length" class="collection-tags">
          <button
            v-for="c in collections.items"
            :key="c.id"
            type="button"
            class="tag"
            :class="collections.contains(c.id, book.id) ? 'tag-accent' : 'tag-outline'"
            @click="collections.toggleBook(c.id, book.id)"
          >
            {{ c.name }}
          </button>
        </div>
        <p v-else class="stranded-note">No collections yet — create one on the Collections tab.</p>
      </div>

      <p v-if="error" class="form-error">{{ error }}</p>

      <div class="dialog-actions">
        <button class="btn btn-secondary danger" type="button" :disabled="busy" @click="removeFromLibrary">
          Remove
        </button>
        <button class="btn btn-secondary" type="button" @click="ui.closeBook()">Close</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.book-dialog { width: min(480px, 100%); }
.dialog-head { display: flex; gap: 16px; }
.dialog-head-text { flex: 1; min-width: 0; }
.author { font-size: 13px; }
.meta { font-size: 12px; margin-top: 4px; }

.field-label {
  font-size: 12px;
  color: color-mix(in srgb, var(--color-text) 70%, transparent);
  margin-bottom: 6px;
}

.status-row { display: flex; gap: 6px; }
.status-btn {
  flex: 1;
  padding: 8px;
  border-radius: var(--radius-md);
  font-size: 13px;
  font-family: var(--font-body);
  cursor: pointer;
  border: 1px solid var(--color-divider);
  background: transparent;
  color: var(--color-text);
}
.status-btn:hover:not(:disabled) { background: color-mix(in srgb, var(--color-text) 6%, transparent); }
.status-btn.is-active { border-color: var(--color-accent); color: var(--color-accent); }
.status-btn:disabled { opacity: 0.6; cursor: default; }

.stars { display: flex; gap: 4px; }
.star {
  border: none;
  background: transparent;
  cursor: pointer;
  font-size: 24px;
  padding: 0;
  line-height: 1;
  color: var(--color-neutral-300);
}
.star.is-on { color: var(--color-accent); }
.star:disabled { cursor: not-allowed; opacity: 0.5; }

.collection-tags { display: flex; flex-wrap: wrap; gap: 6px; }
.collection-tags .tag { cursor: pointer; border-width: 1px; font-family: var(--font-body); }

.dialog-actions { justify-content: space-between; }
.danger { color: var(--color-accent-700); }
</style>
