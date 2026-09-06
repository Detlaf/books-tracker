import { defineStore } from 'pinia'
import { ref, watch } from 'vue'
import { load, save } from '@/lib/localStore'

// TODO(backend): the API has no profile or preferences endpoint — /me returns
// only user_id, and there is no roadmap milestone for a display name or an
// annual goal. Both are prototype-only fields, so they live in this browser.
//
// The email shown on this screen is the address the user signed in with, held
// by the auth store; editing it here changes the label only, not the account.

const DEFAULT_GOAL = 24

export const useSettingsStore = defineStore('settings', () => {
  const displayName = ref('')
  const readingGoal = ref(DEFAULT_GOAL)

  function hydrate(fallbackName = '') {
    const stored = load('settings', null)
    displayName.value = stored?.displayName || fallbackName
    readingGoal.value = stored?.readingGoal || DEFAULT_GOAL
  }

  watch([displayName, readingGoal], () => {
    save('settings', { displayName: displayName.value, readingGoal: readingGoal.value })
  })

  function setGoal(value) {
    const n = parseInt(value, 10)
    readingGoal.value = Number.isNaN(n) || n < 1 ? 1 : n
  }

  function reset() {
    displayName.value = ''
    readingGoal.value = DEFAULT_GOAL
  }

  return { displayName, readingGoal, hydrate, setGoal, reset }
})
