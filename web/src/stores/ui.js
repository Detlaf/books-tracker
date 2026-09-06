import { defineStore } from 'pinia'
import { ref } from 'vue'

// Which book the detail dialog is showing. It lives in a store rather than in
// each view because the dialog is mounted once in App.vue and opened from both
// the library grid and the search results.
export const useUiStore = defineStore('ui', () => {
  const selectedBookId = ref(null)

  function openBook(bookId) {
    selectedBookId.value = bookId
  }

  function closeBook() {
    selectedBookId.value = null
  }

  return { selectedBookId, openBook, closeBook }
})
