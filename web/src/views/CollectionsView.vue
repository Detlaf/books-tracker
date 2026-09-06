<script setup>
import { ref, computed } from 'vue'
import { useCollectionsStore } from '@/stores/collections'
import { useLibraryStore } from '@/stores/library'
import { coverStyle, coverInitial } from '@/lib/covers'

const collections = useCollectionsStore()
const library = useLibraryStore()

const showForm = ref(false)
const newName = ref('')

const cards = computed(() =>
  collections.items.map((c) => ({
    id: c.id,
    name: c.name,
    count: c.bookIds.length,
    // Only books still in the library can be shown; a removed one leaves the
    // count honest but has no cover to draw.
    thumbs: c.bookIds
      .map((id) => library.byBookId.get(id))
      .filter(Boolean)
      .slice(0, 4)
      .map((e) => ({ id: e.book.id, title: e.book.title })),
  })),
)

function create() {
  if (collections.create(newName.value)) {
    newName.value = ''
    showForm.value = false
  }
}
</script>

<template>
  <div class="head">
    <h1 class="page-title">Collections</h1>
    <button class="btn btn-primary" type="button" @click="showForm = !showForm">+ New collection</button>
  </div>
  <p class="text-muted page-subtitle">Group books however you like.</p>

  <div v-if="showForm" class="card new-form">
    <div class="field">
      <label for="name">Collection name</label>
      <input id="name" v-model="newName" class="input" type="text" @keyup.enter="create" />
    </div>
    <button class="btn btn-primary" type="button" @click="create">Create</button>
  </div>

  <div class="book-grid wide">
    <RouterLink
      v-for="c in cards"
      :key="c.id"
      :to="{ name: 'collection', params: { id: c.id } }"
      class="collection-card"
    >
      <div class="thumbs">
        <div
          v-for="t in c.thumbs"
          :key="t.id"
          class="thumb"
          :style="coverStyle(t.id)"
        >
          {{ coverInitial(t.title) }}
        </div>
      </div>
      <div class="card-title">{{ c.name }}</div>
      <div class="card-meta">{{ c.count }} {{ c.count === 1 ? 'book' : 'books' }}</div>
    </RouterLink>
  </div>

  <p v-if="!cards.length" class="text-muted empty">
    No collections yet — create one to start grouping books.
  </p>
  <p class="stranded-note">Collections are saved in this browser only — backend Milestone 6.</p>
</template>

<style scoped>
.head { display: flex; align-items: baseline; justify-content: space-between; gap: 16px; margin-bottom: 6px; }
.new-form { max-width: 380px; margin-bottom: 24px; }
.wide { grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); }

.collection-card {
  display: block;
  border: 1px solid var(--color-divider);
  border-radius: var(--radius-md);
  padding: 16px;
  color: inherit;
  text-decoration: none;
}
.collection-card:hover { box-shadow: var(--shadow-sm); }

.thumbs { display: flex; margin-bottom: 12px; min-height: 60px; }
.thumb {
  width: 44px;
  height: 60px;
  margin-right: -14px;
  border-radius: var(--radius-sm);
  border: 2px solid var(--color-bg);
  display: flex;
  align-items: center;
  justify-content: center;
  font-family: var(--font-heading);
  font-weight: 600;
  font-size: 16px;
}
.empty { margin-top: 8px; }
</style>
