import { defineStore } from 'pinia'
import { ref, watch } from 'vue'
import { load, save } from '@/lib/localStore'

// TODO(backend M5): ratings have no API yet. specs/backend/implementation.md
// specifies PUT/DELETE /library/:book_id/rating with a 1-5 value, valid only
// when the entry's status is read — and the `ratings` table already exists
// from migration 000001. Until those endpoints land, ratings live in this
// browser only: they do not sync across devices and they are lost if site data
// is cleared. Swapping this store's body for API calls is the whole migration;
// nothing outside it knows where the numbers come from.

export const useRatingsStore = defineStore('ratings', () => {
  const byBookId = ref({})

  function hydrate() {
    byBookId.value = load('ratings', {})
  }

  watch(byBookId, (v) => save('ratings', v), { deep: true })

  function get(bookId) {
    return byBookId.value[bookId] ?? 0
  }

  // Sets the exact value. Toggling off is the caller's decision — the star
  // widget treats a click on the current rating as "clear", but an import must
  // not silently undo a rating it is re-applying.
  function set(bookId, rating) {
    const next = { ...byBookId.value }
    if (!rating) delete next[bookId]
    else next[bookId] = Math.max(1, Math.min(5, rating))
    byBookId.value = next
  }

  function clear(bookId) {
    const next = { ...byBookId.value }
    delete next[bookId]
    byBookId.value = next
  }

  function reset() {
    byBookId.value = {}
  }

  return { byBookId, hydrate, get, set, clear, reset }
})
