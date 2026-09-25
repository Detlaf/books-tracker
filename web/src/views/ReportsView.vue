<script setup>
import { computed, onMounted } from 'vue'
import { useStatsStore } from '@/stores/stats'
import { useSettingsStore } from '@/stores/settings'
import { barHeight, barColor, pct, initial, yoyLabel } from '@/lib/reports'

const stats = useStatsStore()
const settings = useSettingsStore()

// Reports has no reactive link to the library anymore (stats are fetched,
// not derived), so refresh on every mount — not just at login — to pick up
// changes made elsewhere (marking a book read, editing a finish date, etc.)
// since the store was last loaded. stats.loaded/stats.loading already keep
// previously-loaded data on screen while this refresh is in flight.
onMounted(() => {
  stats.load()
})

const MONTH_LABELS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']

const years = computed(() => stats.byYear.years.map((y) => y.year))

const yearBars = computed(() => {
  const max = Math.max(1, ...stats.byYear.years.map((y) => y.count))
  const currentYear = String(stats.summary?.currentYear ?? '')
  return stats.byYear.years.map((y) => ({
    year: y.year,
    count: y.count,
    height: barHeight(y.count, max, { min: 10, scale: 110 }),
    color: barColor(y.year === currentYear),
  }))
})

const monthBars = computed(() => {
  const max = Math.max(1, ...stats.byMonth.months.map((m) => m.count))
  return stats.byMonth.months.map((m) => ({
    label: MONTH_LABELS[m.month - 1],
    count: m.count,
    height: barHeight(m.count, max, { min: 6, scale: 70, zeroStub: 2 }),
    color: barColor(m.count > 0, { on: 'var(--color-accent-500)', off: 'var(--color-neutral-200)' }),
  }))
})

const yoy = computed(() =>
  stats.summary ? yoyLabel(stats.summary.thisYearCount, stats.summary.lastYearCount) : '',
)

const scopeLabel = computed(() => (stats.scope === 'all' ? 'all time' : stats.scope))

const goalPct = computed(() =>
  stats.summary
    ? Math.min(100, Math.round((stats.summary.thisYearCount / Math.max(1, settings.readingGoal)) * 100))
    : 0,
)

const languageBars = computed(() => {
  const max = Math.max(1, ...stats.byLanguage.map((l) => l.count))
  return stats.byLanguage.map((l) => ({ ...l, pct: pct(l.count, max) }))
})

const authorRows = computed(() => stats.topAuthors.map((a) => ({ ...a, initial: initial(a.author) })))

function selectScope(value) {
  stats.setScope(value)
}
</script>

<template>
  <h1 class="page-title">Reports</h1>
  <p class="text-muted page-subtitle">Your reading, by the numbers.</p>

  <p v-if="stats.loading && !stats.loaded" class="text-muted">Loading…</p>

  <template v-else-if="stats.summary">
    <div class="stat-grid">
      <div class="card">
        <div class="card-kicker">Total read</div>
        <div class="stat-value">{{ stats.summary.totalRead }}</div>
        <div class="card-meta">all time</div>
      </div>
      <div class="card">
        <div class="card-kicker">This year</div>
        <div class="stat-value">{{ stats.summary.thisYearCount }}</div>
        <div class="card-meta">{{ yoy }}</div>
      </div>
      <div class="card">
        <div class="card-kicker">Current streak</div>
        <div class="stat-value">{{ stats.streak }}</div>
        <div class="card-meta">months in a row with a finish</div>
      </div>
      <div class="card">
        <div class="card-kicker">{{ stats.summary.currentYear }} goal</div>
        <div class="stat-value">{{ stats.summary.thisYearCount }} / {{ settings.readingGoal }}</div>
        <div class="meter goal-meter"><span :style="{ width: goalPct + '%' }" /></div>
      </div>
    </div>

    <h3 class="section-heading">Books finished by year</h3>
    <div v-if="yearBars.length" class="year-chart">
      <div v-for="y in yearBars" :key="y.year" class="year-col">
        <div class="year-count">{{ y.count }}</div>
        <div class="year-bar" :style="{ background: y.color, height: y.height + 'px' }" />
        <div class="text-muted year-label">{{ y.year }}</div>
      </div>
    </div>
    <p v-else class="text-muted chart-empty">Nothing finished yet — mark a book as read to start the chart.</p>

    <!--
      /stats/by-year's rule: a read book with no finish date counts toward
      the summary but cannot be placed in a year. Saying so keeps the
      by-year total reconcilable against "Total read" instead of looking
      like a bug.
    -->
    <p v-if="stats.byYear.undated" class="text-muted excluded-note">
      {{ stats.byYear.undated }} read
      {{ stats.byYear.undated === 1 ? 'book has' : 'books have' }} no finish date and
      {{ stats.byYear.undated === 1 ? 'is' : 'are' }} not shown in the year and month charts.
    </p>

    <h3 class="section-heading">{{ stats.byMonth.year }} by month</h3>
    <div class="month-chart">
      <div v-for="m in monthBars" :key="m.label" class="month-col">
        <div class="month-bar" :style="{ background: m.color, height: m.height + 'px' }" />
        <div class="text-muted month-label">{{ m.label }}</div>
      </div>
    </div>

    <p v-if="stats.error" class="text-muted">{{ stats.error }}</p>

    <div class="scope-row">
      <button type="button" class="pill-sm" :class="{ 'is-active': stats.scope === 'all' }" @click="selectScope('all')">
        All time
      </button>
      <button
        v-for="y in years"
        :key="y"
        type="button"
        class="pill-sm"
        :class="{ 'is-active': stats.scope === y }"
        @click="selectScope(y)"
      >
        {{ y }}
      </button>
    </div>

    <div class="breakdowns">
      <div>
        <h3 class="section-heading">By language ({{ scopeLabel }})</h3>
        <div class="bars">
          <div v-for="l in languageBars" :key="l.language">
            <div class="bar-head">
              <span>{{ l.language }}</span>
              <span class="text-muted">{{ l.count }}</span>
            </div>
            <div class="meter"><span :style="{ width: l.pct + '%' }" /></div>
          </div>
          <p v-if="!languageBars.length" class="text-muted">No finished books in this period.</p>
        </div>
      </div>

      <div>
        <h3 class="section-heading">Most-read authors ({{ scopeLabel }})</h3>
        <div class="bars">
          <div v-for="a in authorRows" :key="a.author" class="author-row">
            <div class="author-avatar">{{ a.initial }}</div>
            <div class="author-name">{{ a.author }}</div>
            <span class="tag tag-neutral">{{ a.count }}</span>
          </div>
          <p v-if="!authorRows.length" class="text-muted">No finished books in this period.</p>
        </div>
      </div>
    </div>
  </template>
  <p v-else-if="stats.error" class="text-muted">{{ stats.error }}</p>
</template>

<style scoped>
.goal-meter { height: 6px; border-radius: 3px; margin-top: 4px; }
.goal-meter > span { background: var(--color-accent); }

.year-chart {
  display: flex;
  align-items: flex-end;
  gap: 18px;
  height: 140px;
  margin-bottom: 10px;
  border-bottom: 1px solid var(--color-divider);
}
.year-col { display: flex; flex-direction: column; align-items: center; gap: 6px; width: 60px; }
.year-count { font-size: 12px; color: var(--color-accent-700); font-weight: 600; }
.year-bar { width: 36px; border-radius: 2px 2px 0 0; }
.year-label { font-size: 12px; }
.chart-empty { margin-bottom: 24px; }
.excluded-note { font-size: 12px; margin: 0 0 24px; }

.month-chart {
  display: flex;
  align-items: flex-end;
  gap: 6px;
  height: 90px;
  margin-bottom: 34px;
  border-bottom: 1px solid var(--color-divider);
}
.month-col { display: flex; flex-direction: column; align-items: center; gap: 4px; flex: 1; }
.month-bar { width: 100%; max-width: 26px; border-radius: 2px 2px 0 0; }
.month-label { font-size: 10px; }

.scope-row { display: flex; gap: 16px; margin-bottom: 8px; flex-wrap: wrap; }

.breakdowns { display: grid; grid-template-columns: 1fr 1fr; gap: 40px; margin-top: 24px; }
.bars { display: flex; flex-direction: column; gap: 10px; }
.bar-head { display: flex; justify-content: space-between; font-size: 13px; margin-bottom: 3px; }

.author-row { display: flex; align-items: center; gap: 10px; }
.author-avatar {
  width: 30px;
  height: 30px;
  border-radius: 50%;
  background: var(--color-accent-100);
  color: var(--color-accent-800);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 12px;
  font-family: var(--font-heading);
  font-weight: 600;
  flex: none;
}
.author-name { flex: 1; font-size: 13px; }

@media (max-width: 760px) {
  .breakdowns { grid-template-columns: 1fr; gap: 24px; }
}
</style>
