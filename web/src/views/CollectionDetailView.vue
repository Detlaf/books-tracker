<script setup>
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useCollectionsStore } from '@/stores/collections'
import { useLibraryStore } from '@/stores/library'
import BookCover from '@/components/BookCover.vue'

const props = defineProps({ id: { type: String, required: true } })

const router = useRouter()
const collections = useCollectionsStore()
const library = useLibraryStore()

const collection = computed(() => collections.find(props.id))
const dragOver = ref(false)

const books = computed(() =>
  (collection.value?.bookIds ?? [])
    .map((id) => library.byBookId.get(id))
    .filter(Boolean)
    .map((e) => e.book),
)

// The left rail lists everything not already in this collection, so dropping a
// duplicate is not something the user can attempt in the first place.
const available = computed(() => {
  const inCollection = new Set(collection.value?.bookIds ?? [])
  return library.entries.filter((e) => !inCollection.has(e.book.id)).map((e) => e.book)
})

function onDragStart(event, bookId) {
  event.dataTransfer.setData('text/plain', String(bookId))
  event.dataTransfer.effectAllowed = 'copy'
}

function onDrop(event) {
  dragOver.value = false
  const raw = event.dataTransfer.getData('text/plain')
  const bookId = Number(raw)
  // Book ids are integers from the API; anything else came from a drag that
  // did not start in this rail.
  if (!raw || Number.isNaN(bookId)) return
  collections.addBook(props.id, bookId)
}

function renameCollection() {
  const next = window.prompt('Rename collection', collection.value.name)
  if (next !== null) collections.rename(props.id, next)
}

function deleteCollection() {
  if (!window.confirm(`Delete "${collection.value.name}"? The books stay in your library.`)) return
  collections.remove(props.id)
  router.push({ name: 'collections' })
}
</script>

<template>
  <template v-if="collection">
    <RouterLink :to="{ name: 'collections' }" class="back">&larr; All collections</RouterLink>

    <div class="head">
      <h1 class="detail-title">{{ collection.name }}</h1>
      <div class="head-actions">
        <button class="btn btn-secondary" type="button" @click="renameCollection">Rename</button>
        <button class="btn btn-secondary danger" type="button" @click="deleteCollection">Delete</button>
      </div>
    </div>
    <p class="text-muted page-subtitle">Drag a book from your library onto the collection to add it.</p>

    <div class="layout">
      <div>
        <h3 class="rail-heading">Your library</h3>
        <div class="rail">
          <div
            v-for="book in available"
            :key="book.id"
            class="rail-item"
            draggable="true"
            @dragstart="onDragStart($event, book.id)"
          >
            <BookCover :book="book" width="24px" height="32px" font-size="12px" />
            <span class="rail-title">{{ book.title }}</span>
            <button class="rail-add" type="button" @click="collections.addBook(props.id, book.id)">+</button>
          </div>
          <p v-if="!available.length" class="text-muted rail-empty">Every book you track is already here.</p>
        </div>
      </div>

      <div
        class="dropzone"
        :class="{ 'is-over': dragOver }"
        @dragover.prevent="dragOver = true"
        @dragleave="dragOver = false"
        @drop.prevent="onDrop"
      >
        <div class="book-grid narrow">
          <div v-for="book in books" :key="book.id" class="member">
            <button
              class="remove"
              type="button"
              :aria-label="`Remove ${book.title}`"
              @click="collections.removeBook(props.id, book.id)"
            >
              &times;
            </button>
            <BookCover :book="book" height="110px" font-size="28px" />
            <div class="card-title member-title">{{ book.title }}</div>
            <div class="text-muted member-author">{{ book.authors.join(', ') || 'Unknown author' }}</div>
          </div>
        </div>
        <p v-if="!books.length" class="text-muted drop-hint">Drop books here.</p>
      </div>
    </div>
  </template>

  <template v-else>
    <h1 class="page-title">Collection not found</h1>
    <p class="text-muted">
      It may have been deleted.
      <RouterLink :to="{ name: 'collections' }">Back to collections</RouterLink>.
    </p>
  </template>
</template>

<style scoped>
.back {
  display: inline-block;
  color: var(--color-accent);
  font-size: 13px;
  text-decoration: none;
  margin-bottom: 12px;
}
.head { display: flex; align-items: baseline; justify-content: space-between; gap: 16px; }
.head-actions { display: flex; gap: 8px; flex: none; }
.detail-title { font-size: 30px; margin: 0 0 6px; }
.danger { color: var(--color-accent-700); }

.layout { display: grid; grid-template-columns: 260px 1fr; gap: 32px; }

.rail-heading {
  font-size: 13px;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--color-accent-700);
  margin: 0 0 12px;
}
.rail { display: flex; flex-direction: column; gap: 8px; max-height: 70vh; overflow-y: auto; }
.rail-item {
  cursor: grab;
  display: flex;
  align-items: center;
  gap: 10px;
  border: 1px solid var(--color-divider);
  border-radius: var(--radius-md);
  padding: 8px 10px;
  font-size: 13px;
}
.rail-item:active { cursor: grabbing; }
.rail-title { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; flex: 1; }
/* Dragging is the design's interaction, but it is mouse-only; the + gives
   keyboard and touch users the same operation. */
.rail-add {
  border: none;
  background: transparent;
  color: var(--color-accent);
  cursor: pointer;
  font-size: 16px;
  line-height: 1;
  padding: 0 2px;
  flex: none;
}
.rail-empty { font-size: 12px; }

.dropzone {
  border: 1px dashed var(--color-divider);
  border-radius: var(--radius-md);
  padding: 20px;
  min-height: 300px;
}
.dropzone.is-over { border-color: var(--color-accent); background: color-mix(in srgb, var(--color-accent) 6%, transparent); }
.narrow { grid-template-columns: repeat(auto-fill, minmax(150px, 1fr)); gap: 16px; }

.member { position: relative; border: 1px solid var(--color-divider); border-radius: var(--radius-md); padding: 10px; }
.remove {
  position: absolute;
  top: 6px;
  right: 6px;
  border: none;
  background: var(--color-surface);
  color: var(--color-text);
  border-radius: 50%;
  width: 20px;
  height: 20px;
  cursor: pointer;
  font-size: 12px;
  line-height: 1;
  z-index: 1;
}
.member-title { font-size: 13px; margin-top: 8px; }
.member-author { font-size: 11px; }
.drop-hint { text-align: center; margin-top: 40px; }

@media (max-width: 760px) {
  .layout { grid-template-columns: 1fr; gap: 20px; }
  .rail { max-height: 240px; }
}
</style>
