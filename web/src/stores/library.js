import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import * as libraryApi from '@/api/library'

const PAGE_SIZE = 100

export const useLibraryStore = defineStore('library', () => {
  const entries = ref([])
  const loading = ref(false)
  const error = ref('')
  const loaded = ref(false)

  const byBookId = computed(() => new Map(entries.value.map((e) => [e.book.id, e])))
  const count = computed(() => entries.value.length)

  const counts = computed(() => ({
    all: entries.value.length,
    backlog: entries.value.filter((e) => e.status === 'backlog').length,
    reading: entries.value.filter((e) => e.status === 'reading').length,
    read: entries.value.filter((e) => e.status === 'read').length,
  }))

  function filtered(status) {
    if (status === 'all') return entries.value
    return entries.value.filter((e) => e.status === status)
  }

  // The whole library is fetched, not a page of it: the Reports tab and the
  // collection picker both need every entry, and paging them lazily would mean
  // statistics that change as you scroll. Pages are pulled until one comes
  // back short.
  async function fetchAll() {
    loading.value = true
    error.value = ''
    try {
      const all = []
      for (let page = 1; ; page++) {
        const { items } = await libraryApi.list({ page, limit: PAGE_SIZE, sort: '-added_at' })
        all.push(...items)
        if (items.length < PAGE_SIZE) break
      }
      entries.value = all
      loaded.value = true
    } catch (e) {
      error.value = e.message || 'Could not load your library.'
    } finally {
      loading.value = false
    }
  }

  function upsert(entry) {
    const i = entries.value.findIndex((e) => e.book.id === entry.book.id)
    if (i >= 0) entries.value[i] = entry
    else entries.value.unshift(entry)
  }

  async function add(bookId, status, finishedAt) {
    const entry = await libraryApi.add(bookId, status, finishedAt)
    upsert(entry)
    return entry
  }

  async function setStatus(bookId, status) {
    // finished_at is left out entirely: the backend owns the transition rules
    // (stamping it on the move to read, clearing it on the way back out), and
    // sending a value here would fight them.
    const entry = await libraryApi.update(bookId, { status })
    upsert(entry)
    return entry
  }

  async function setFinishedAt(bookId, finishedAt) {
    const entry = await libraryApi.update(bookId, { finishedAt })
    upsert(entry)
    return entry
  }

  async function remove(bookId) {
    await libraryApi.remove(bookId)
    entries.value = entries.value.filter((e) => e.book.id !== bookId)
  }

  function reset() {
    entries.value = []
    loaded.value = false
    error.value = ''
  }

  return {
    entries, loading, error, loaded,
    byBookId, count, counts, filtered,
    fetchAll, add, setStatus, setFinishedAt, remove, reset,
  }
})
