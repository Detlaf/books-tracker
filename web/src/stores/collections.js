import { defineStore } from 'pinia'
import { ref, watch } from 'vue'
import { load, save } from '@/lib/localStore'

// TODO(backend M6): collections have no API yet.
// specs/backend/implementation.md specifies GET/POST /collections,
// PATCH/DELETE /collections/:id and POST/DELETE /collections/:id/books, and
// the `collections` / `collection_books` tables already exist from migration
// 000001. Until then collections live in this browser only.
//
// Ids are generated locally as `local-<timestamp>`; when the endpoints land
// they will be server-assigned integers, so any migration path has to treat a
// `local-` prefix as "not yet uploaded".

export const useCollectionsStore = defineStore('collections', () => {
  const items = ref([])

  function hydrate() {
    items.value = load('collections', [])
  }

  watch(items, (v) => save('collections', v), { deep: true })

  function create(name) {
    const trimmed = name.trim()
    if (!trimmed) return null
    const collection = { id: `local-${Date.now()}`, name: trimmed, bookIds: [] }
    items.value = [...items.value, collection]
    return collection
  }

  function rename(id, name) {
    const trimmed = name.trim()
    if (!trimmed) return
    items.value = items.value.map((c) => (c.id === id ? { ...c, name: trimmed } : c))
  }

  function remove(id) {
    items.value = items.value.filter((c) => c.id !== id)
  }

  function find(id) {
    return items.value.find((c) => c.id === id) ?? null
  }

  function addBook(collectionId, bookId) {
    if (bookId == null) return
    items.value = items.value.map((c) =>
      c.id !== collectionId || c.bookIds.includes(bookId)
        ? c
        : { ...c, bookIds: [...c.bookIds, bookId] },
    )
  }

  function removeBook(collectionId, bookId) {
    items.value = items.value.map((c) =>
      c.id !== collectionId ? c : { ...c, bookIds: c.bookIds.filter((id) => id !== bookId) },
    )
  }

  function toggleBook(collectionId, bookId) {
    const c = find(collectionId)
    if (!c) return
    if (c.bookIds.includes(bookId)) removeBook(collectionId, bookId)
    else addBook(collectionId, bookId)
  }

  function contains(collectionId, bookId) {
    return find(collectionId)?.bookIds.includes(bookId) ?? false
  }

  // Called when a book leaves the library, so a collection cannot keep
  // pointing at an entry that is gone.
  function forgetBook(bookId) {
    items.value = items.value.map((c) => ({ ...c, bookIds: c.bookIds.filter((id) => id !== bookId) }))
  }

  function reset() {
    items.value = []
  }

  return {
    items, hydrate, create, rename, remove, find,
    addBook, removeBook, toggleBook, contains, forgetBook, reset,
  }
})
