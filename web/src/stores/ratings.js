import { defineStore } from 'pinia'
import { useLibraryStore } from '@/stores/library'
import { setRating, clearRating } from '@/api/library'

// Ratings are folded into library entries server-side (backend Milestone 5):
// GET/POST/PATCH /library already return "rating", so this store holds no
// state of its own — it reads through useLibraryStore() and writes through
// the rating endpoints, patching the cached entry rather than refetching.

export const useRatingsStore = defineStore('ratings', () => {
  function get(bookId) {
    return useLibraryStore().byBookId.get(bookId)?.rating ?? 0
  }

  // Sets the exact value. Toggling off is the caller's decision — the star
  // widget treats a click on the current rating as "clear", but an import
  // must not silently undo a rating it is re-applying.
  async function set(bookId, rating) {
    const library = useLibraryStore()
    if (!rating) {
      await clearRating(bookId)
      library.applyRating(bookId, null)
      return
    }
    const clamped = Math.max(1, Math.min(5, rating))
    await setRating(bookId, clamped)
    library.applyRating(bookId, clamped)
  }

  async function clear(bookId) {
    await clearRating(bookId)
    useLibraryStore().applyRating(bookId, null)
  }

  return { get, set, clear }
})
