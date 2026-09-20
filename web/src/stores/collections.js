import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as collectionsApi from '@/api/collections'

// Collections are backed by the API (backend Milestone 6). IDs are compared
// as strings throughout: route params (CollectionDetailView's props.id) are
// always strings, but the API returns numeric ids.
//
// The wire shape uses book_ids; this store keeps the field as bookIds so no
// view needs to change — normalize() is the only place the two names meet.
function normalize(c) {
  return { id: c.id, name: c.name, bookIds: c.book_ids }
}

export const useCollectionsStore = defineStore('collections', () => {
  const items = ref([])

  async function hydrate() {
    const { items: fetched } = await collectionsApi.list()
    items.value = fetched.map(normalize)
  }

  async function create(name) {
    const trimmed = name.trim()
    if (!trimmed) return null
    const created = await collectionsApi.create(trimmed)
    const collection = normalize(created)
    items.value = [...items.value, collection]
    return collection
  }

  async function rename(id, name) {
    const trimmed = name.trim()
    if (!trimmed) return
    const updated = await collectionsApi.rename(id, trimmed)
    items.value = items.value.map((c) => (String(c.id) === String(id) ? normalize(updated) : c))
  }

  async function remove(id) {
    await collectionsApi.remove(id)
    items.value = items.value.filter((c) => String(c.id) !== String(id))
  }

  function find(id) {
    return items.value.find((c) => String(c.id) === String(id)) ?? null
  }

  async function addBook(collectionId, bookId) {
    if (bookId == null) return
    const updated = await collectionsApi.addBook(collectionId, bookId)
    items.value = items.value.map((c) =>
      String(c.id) === String(collectionId) ? normalize(updated) : c,
    )
  }

  async function removeBook(collectionId, bookId) {
    await collectionsApi.removeBook(collectionId, bookId)
    items.value = items.value.map((c) =>
      String(c.id) !== String(collectionId)
        ? c
        : { ...c, bookIds: c.bookIds.filter((id) => id !== bookId) },
    )
  }

  async function toggleBook(collectionId, bookId) {
    const c = find(collectionId)
    if (!c) return
    if (c.bookIds.includes(bookId)) await removeBook(collectionId, bookId)
    else await addBook(collectionId, bookId)
  }

  function contains(collectionId, bookId) {
    return find(collectionId)?.bookIds.includes(bookId) ?? false
  }

  // Called when a book leaves the library, so a collection cannot keep
  // pointing at an entry that is gone. This is a local-only cache update —
  // the backend has no "remove this book from every collection" call, and
  // none is needed: the book row staying out of user_books does not orphan
  // collection_books rows in a way that matters to this client.
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
