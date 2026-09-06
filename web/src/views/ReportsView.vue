<script setup>
import { ref, computed } from 'vue'
import { useLibraryStore } from '@/stores/library'
import { useSettingsStore } from '@/stores/settings'
import * as reports from '@/lib/reports'

const library = useLibraryStore()
const settings = useSettingsStore()

const scope = ref('all')

const entries = computed(() => library.entries)
const summary = computed(() => reports.summary(entries.value))
const byYear = computed(() => reports.byYear(entries.value))
const monthly = computed(() => reports.byMonth(entries.value, summary.value.currentYear))
const streak = computed(() => reports.currentStreak(entries.value))
const years = computed(() => reports.availableYears(entries.value))
const byLanguage = computed(() => reports.byLanguage(entries.value, scope.value))
const topAuthors = computed(() => reports.topAuthors(entries.value, scope.value))

const scopeLabel = computed(() => (scope.value === 'all' ? 'all time' : scope.value))
const goalPct = computed(() =>
  Math.min(100, Math.round((summary.value.thisYearCount / Math.max(1, settings.readingGoal)) * 100)),
)
</script>

<template>
  <h1 class="page-title">Reports</h1>
  <p class="text-muted page-subtitle">Your reading, by the numbers.</p>

  <p v-if="library.loading && !library.loaded" class="text-muted">Loading…</p>

  <template v-else>
    <div class="stat-grid">
      <div class="card">
        <div class="card-kicker">Total read</div>
        <div class="stat-value">{{ summary.totalRead }}</div>
        <div class="card-meta">all time</div>
      </div>
      <div class="card">
        <div class="card-kicker">This year</div>
        <div class="stat-value">{{ summary.thisYearCount }}</div>
        <div class="card-meta">{{ summary.yoyLabel }}</div>
      </div>
      <div class="card">
        <div class="card-kicker">Current streak</div>
        <div class="stat-value">{{ streak }}</div>
        <div class="card-meta">months in a row with a finish</div>
      </div>
      <div class="card">
        <div class="card-kicker">{{ summary.currentYear }} goal</div>
        <div class="stat-value">{{ summary.thisYearCount }} / {{ settings.readingGoal }}</div>
        <div class="meter goal-meter"><span :style="{ width: goalPct + '%' }" /></div>
      </div>
    </div>

    <h3 class="section-heading">Books finished by year</h3>
    <div v-if="byYear.length" class="year-chart">
      <div v-for="y in byYear" :key="y.year" class="year-col">
        <div class="year-count">{{ y.count }}</div>
        <div class="year-bar" :style="{ background: y.barColor, height: y.barHeight + 'px' }" />
        <div class="text-muted year-label">{{ y.year }}</div>
      </div>
    </div>
    <p v-else class="text-muted chart-empty">Nothing finished yet — mark a book as read to start the chart.</p>

    <!--
      Backend Milestone 7 specifies that read books with no finish date are
      counted in the summary but cannot be placed in a year. Saying so keeps
      the by-year total reconcilable against "Total read" instead of looking
      like a bug.
    -->
    <p v-if="summary.undatedRead" class="text-muted excluded-note">
      {{ summary.undatedRead }} read
      {{ summary.undatedRead === 1 ? 'book has' : 'books have' }} no finish date and
      {{ summary.undatedRead === 1 ? 'is' : 'are' }} not shown in the year and month charts.
    </p>

    <h3 class="section-heading">{{ summary.currentYear }} by month</h3>
    <div class="month-chart">
      <div v-for="m in monthly" :key="m.label" class="month-col">
        <div class="month-bar" :style="{ background: m.barColor, height: m.barHeight + 'px' }" />
        <div class="text-muted month-label">{{ m.label }}</div>
      </div>
    </div>

    <div class="scope-row">
      <button type="button" class="pill-sm" :class="{ 'is-active': scope === 'all' }" @click="scope = 'all'">
        All time
      </button>
      <button
        v-for="y in years"
        :key="y"
        type="button"
        class="pill-sm"
        :class="{ 'is-active': scope === y }"
        @click="scope = y"
      >
        {{ y }}
      </button>
    </div>

    <div class="breakdowns">
      <div>
        <h3 class="section-heading">By language ({{ scopeLabel }})</h3>
        <div class="bars">
          <div v-for="l in byLanguage" :key="l.language">
            <div class="bar-head">
              <span>{{ l.language }}</span>
              <span class="text-muted">{{ l.count }}</span>
            </div>
            <div class="meter"><span :style="{ width: l.pct + '%' }" /></div>
          </div>
          <p v-if="!byLanguage.length" class="text-muted">No finished books in this period.</p>
        </div>
      </div>

      <div>
        <h3 class="section-heading">Most-read authors ({{ scopeLabel }})</h3>
        <div class="bars">
          <div v-for="a in topAuthors" :key="a.author" class="author-row">
            <div class="author-avatar">{{ a.initial }}</div>
            <div class="author-name">{{ a.author }}</div>
            <span class="tag tag-neutral">{{ a.count }}</span>
          </div>
          <p v-if="!topAuthors.length" class="text-muted">No finished books in this period.</p>
        </div>
      </div>
    </div>
  </template>
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
