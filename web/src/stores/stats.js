import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as statsApi from '@/api/stats'

// Backed by backend Milestone 7 (/stats/*). Six figures, one store: summary,
// by-year, by-month, by-language, top-authors and streak. scope drives
// by-language/top-authors; changing it only re-fetches those two, not a
// full reload.
function normalizeSummary(s) {
  return {
    totalRead: s.total_read,
    reading: s.reading,
    backlog: s.backlog,
    thisYearCount: s.this_year,
    lastYearCount: s.last_year,
    currentYear: s.current_year,
    undatedRead: s.undated_read,
  }
}

export const useStatsStore = defineStore('stats', () => {
  const summary = ref(null)
  const byYear = ref({ years: [], undated: 0 })
  const byMonth = ref({ year: '', months: [] })
  const byLanguage = ref([])
  const topAuthors = ref([])
  const streak = ref(0)
  const scope = ref('all')
  const loading = ref(false)
  const loaded = ref(false)
  const error = ref('')

  async function fetchScoped() {
    const scopeParam = scope.value === 'all' ? undefined : scope.value
    const [language, authors] = await Promise.all([
      statsApi.byLanguage(scopeParam),
      statsApi.topAuthors(scopeParam),
    ])
    byLanguage.value = language.languages
    topAuthors.value = authors.authors
  }

  async function load() {
    loading.value = true
    error.value = ''
    try {
      const [s, y, m, streakResult] = await Promise.all([
        statsApi.summary(),
        statsApi.byYear(),
        statsApi.byMonth(),
        statsApi.streak(),
      ])
      summary.value = normalizeSummary(s)
      byYear.value = y
      byMonth.value = m
      streak.value = streakResult.months
      await fetchScoped()
      loaded.value = true
    } catch (e) {
      error.value = e.message || 'Could not load your reading stats.'
    } finally {
      loading.value = false
    }
  }

  async function setScope(next) {
    const previous = scope.value
    error.value = ''
    scope.value = next
    try {
      await fetchScoped()
    } catch (e) {
      scope.value = previous
      error.value = e.message || 'Could not load your reading stats.'
    }
  }

  async function setMonthYear(year) {
    error.value = ''
    try {
      byMonth.value = await statsApi.byMonth(year)
    } catch (e) {
      error.value = e.message || 'Could not load your reading stats.'
    }
  }

  function reset() {
    summary.value = null
    byYear.value = { years: [], undated: 0 }
    byMonth.value = { year: '', months: [] }
    byLanguage.value = []
    topAuthors.value = []
    streak.value = 0
    scope.value = 'all'
    loaded.value = false
    error.value = ''
  }

  return {
    summary, byYear, byMonth, byLanguage, topAuthors, streak, scope,
    loading, loaded, error,
    load, setScope, setMonthYear, reset,
  }
})
