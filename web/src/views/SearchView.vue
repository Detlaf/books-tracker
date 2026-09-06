<script setup>
import { ref, computed, watch, onUnmounted } from 'vue'
import * as booksApi from '@/api/books'
import { ApiError } from '@/api/client'
import { useLibraryStore } from '@/stores/library'
import { STATUSES, statusLabel, statusTagClass } from '@/lib/status'
import BookCover from '@/components/BookCover.vue'

const library = useLibraryStore()

const query = ref('')
const isbnQuery = ref('')
const results = ref([])
const searching = ref(false)
const error = ref('')
const isbnNotFound = ref(false)
const searched = ref(false)
const addingId = ref(null)

// Every keystroke would otherwise be a provider call — and the search route is
// behind auth precisely because each one spends API quota. A trailing debounce
// plus a sequence guard means only the last query in a burst is displayed,
// even if an earlier response lands after it.
let timer = null
let seq = 0

function debounce(fn, ms = 350) {
  clearTimeout(timer)
  timer = setTimeout(fn, ms)
}

onUnmounted(() => clearTimeout(timer))

async function runTextSearch(q) {
  const mine = ++seq
  searching.value = true
  error.value = ''
  isbnNotFound.value = false
  try {
    const { items } = await booksApi.search(q)
    if (mine !== seq) return
    results.value = items
  } catch (e) {
    if (mine !== seq) return
    results.value = []
    error.value = e.message || 'Search failed.'
  } finally {
    if (mine === seq) {
      searching.value = false
      searched.value = true
    }
  }
}

async function runIsbnSearch(raw) {
  const mine = ++seq
  searching.value = true
  error.value = ''
  isbnNotFound.value = false
  try {
    const book = await booksApi.byISBN(raw)
    if (mine !== seq) return
    results.value = [book]
  } catch (e) {
    if (mine !== seq) return
    results.value = []
    // A 404 is an ordinary outcome here, not an error to shout about; a 400
    // (malformed ISBN) and anything upstream are.
    if (e instanceof ApiError && e.status === 404) isbnNotFound.value = true
    else error.value = e.message || 'Lookup failed.'
  } finally {
    if (mine === seq) {
      searching.value = false
      searched.value = true
    }
  }
}

// ISBN wins when both fields have content: it is the more specific of the two.
watch([query, isbnQuery], ([q, isbn]) => {
  const cleanIsbn = isbn.trim().replace(/[\s-]/g, '')
  const cleanQuery = q.trim()

  if (!cleanIsbn && !cleanQuery) {
    seq++
    clearTimeout(timer)
    results.value = []
    searched.value = false
    isbnNotFound.value = false
    error.value = ''
    return
  }

  // A partial ISBN is not worth a lookup; both formats are 10 or 13 digits
  // (with X permitted as the ISBN-10 check character).
  if (cleanIsbn) {
    if (/^[0-9]{9}[0-9Xx]$|^[0-9]{13}$/.test(cleanIsbn)) debounce(() => runIsbnSearch(cleanIsbn))
    return
  }
  debounce(() => runTextSearch(cleanQuery))
})

const showNoResults = computed(
  () => searched.value && !searching.value && !error.value && results.value.length === 0 && !isbnNotFound.value,
)

function entryFor(bookId) {
  return library.byBookId.get(bookId) ?? null
}

async function addTo(book, status) {
  addingId.value = book.id
  error.value = ''
  try {
    await library.add(book.id, status)
  } catch (e) {
    error.value = e.message || 'Could not add that book.'
  } finally {
    addingId.value = null
  }
}
</script>

<template>
  <h1 class="page-title">Search &amp; Add</h1>
  <p class="text-muted page-subtitle">Find a book by title, author, or ISBN.</p>

  <div class="search-fields">
    <div class="field query-field">
      <label for="q">Title or author</label>
      <input id="q" v-model="query" class="input" type="text" placeholder="e.g. Murakami, or Circe" />
    </div>
    <div class="field isbn-field">
      <label for="isbn">ISBN</label>
      <input id="isbn" v-model="isbnQuery" class="input" type="text" placeholder="978-0-…" />
    </div>
  </div>

  <p v-if="error" class="form-error error-line">{{ error }}</p>
  <p v-if="searching" class="text-muted">Searching…</p>

  <!--
    The prototype offered a manual-entry form here. The API has no endpoint for
    creating a book outside the metadata provider — books are only ever
    upserted by /books/search and /books/isbn/:isbn, and POST /library needs an
    id that came from one of those — so a form would have nothing to submit to.
    The card states the situation instead of pretending to accept input.
  -->
  <div v-if="isbnNotFound" class="card not-found">
    <div class="card-kicker">Not found</div>
    <p class="card-body">
      No catalog match for that ISBN. Manual entry needs a backend endpoint for creating a book,
      which does not exist yet — try searching by title or author instead.
    </p>
  </div>

  <div class="results">
    <div v-for="book in results" :key="book.id" class="result">
      <BookCover :book="book" width="52px" height="70px" font-size="20px" />
      <div class="result-text">
        <div class="card-title result-title">{{ book.title }}</div>
        <div class="text-muted result-meta">
          {{ book.authors.join(', ') || 'Unknown author' }}
          <template v-if="book.language"> · {{ book.language.toUpperCase() }}</template>
        </div>
        <div v-if="book.isbn" class="text-muted result-isbn">ISBN {{ book.isbn }}</div>
      </div>

      <span v-if="entryFor(book.id)" class="tag" :class="statusTagClass(entryFor(book.id).status)">
        {{ statusLabel(entryFor(book.id).status) }}
      </span>
      <div v-else class="add-actions">
        <button
          v-for="status in STATUSES"
          :key="status"
          class="btn btn-secondary"
          type="button"
          :disabled="addingId === book.id"
          @click="addTo(book, status)"
        >
          {{ statusLabel(status) }}
        </button>
      </div>
    </div>

    <p v-if="showNoResults" class="text-muted">No matches. Try a different title, author, or ISBN.</p>
  </div>
</template>

<style scoped>
.search-fields { display: flex; gap: 24px; flex-wrap: wrap; margin-bottom: 28px; }
.query-field { flex: 1; min-width: 240px; }
.isbn-field { width: 260px; }
.error-line { margin-bottom: 12px; }
.not-found { margin-bottom: 24px; max-width: 420px; }

.results { display: flex; flex-direction: column; gap: 12px; }
.result {
  display: flex;
  align-items: center;
  gap: 16px;
  border: 1px solid var(--color-divider);
  border-radius: var(--radius-md);
  padding: 14px;
}
.result-text { flex: 1; min-width: 0; }
.result-title { font-size: 15px; }
.result-meta { font-size: 12px; }
.result-isbn { font-size: 11px; margin-top: 2px; }
.add-actions { display: flex; gap: 6px; flex: none; }
</style>
