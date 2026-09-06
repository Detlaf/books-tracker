<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useSettingsStore } from '@/stores/settings'
import { useLibraryStore } from '@/stores/library'
import { useRatingsStore } from '@/stores/ratings'
import { parseImportFile } from '@/lib/import'
import { runImport } from '@/lib/runImport'

const router = useRouter()
const auth = useAuthStore()
const settings = useSettingsStore()
const library = useLibraryStore()
const ratings = useRatingsStore()

const importing = ref(false)
const progress = ref({ done: 0, total: 0 })
const message = ref('')
const detail = ref([])
const fileInput = ref(null)

async function onFile(event) {
  const file = event.target.files?.[0]
  if (!file) return

  importing.value = true
  message.value = ''
  detail.value = []
  progress.value = { done: 0, total: 0 }

  try {
    const records = await parseImportFile(file)
    if (!records.length) {
      message.value = 'No rows with a title were found in that file.'
      return
    }

    progress.value = { done: 0, total: records.length }
    const result = await runImport(records, { library, ratings }, (done, total) => {
      progress.value = { done, total }
    })

    message.value = `Imported: ${result.added} added, ${result.updated} updated.`
    if (result.unmatched.length) {
      detail.value.push(
        `No catalog match for ${result.unmatched.length}: ${result.unmatched.slice(0, 5).join(', ')}${
          result.unmatched.length > 5 ? '…' : ''
        }`,
      )
    }
    if (result.failed.length) {
      detail.value.push(`Failed for ${result.failed.length}: ${result.failed.slice(0, 3).join('; ')}`)
    }
  } catch (e) {
    message.value = e.message || 'Could not read that file.'
  } finally {
    importing.value = false
    // Clearing the input lets the same file be picked again after a fix.
    if (fileInput.value) fileInput.value.value = ''
  }
}

async function signOut() {
  await auth.logout()
  router.push({ name: 'login' })
}
</script>

<template>
  <h1 class="page-title">Settings</h1>
  <p class="text-muted page-subtitle">Your account and reading preferences.</p>

  <div class="stack">
    <div class="card">
      <div class="card-kicker">Profile</div>
      <div class="field spaced">
        <label for="display-name">Display name</label>
        <input id="display-name" v-model="settings.displayName" class="input" type="text" />
      </div>
      <div class="field">
        <label for="account-email">Email</label>
        <input id="account-email" class="input" type="email" :value="auth.email" disabled />
      </div>
      <p class="stranded-note">
        The display name is saved in this browser; the email is your account address and can't be
        changed here.
      </p>
    </div>

    <div class="card">
      <div class="card-kicker">Reading preferences</div>
      <div class="field spaced">
        <label for="goal">Annual reading goal (books)</label>
        <input
          id="goal"
          class="input"
          type="number"
          min="1"
          :value="settings.readingGoal"
          @input="settings.setGoal($event.target.value)"
        />
      </div>
      <p class="stranded-note">Saved in this browser only.</p>
    </div>

    <div class="card">
      <div class="card-kicker">Import reading history</div>
      <p class="card-body spaced">Bring in books from a spreadsheet or text export.</p>
      <input
        ref="fileInput"
        class="input"
        type="file"
        accept=".txt,.csv,.tsv,.xlsx"
        :disabled="importing"
        @change="onFile"
      />
      <p class="stranded-note">
        Columns: Title, Author, Status (to-read/reading/read), Rating (0–5), Date Finished
        (YYYY-MM-DD), Language, ISBN.
      </p>
      <!--
        Each row is matched against the catalog before it can be stored, which
        is a provider call per unmatched book — worth a progress line, because
        a few hundred rows is not instant.
      -->
      <p v-if="importing" class="progress">
        Matching against the catalog… {{ progress.done }} / {{ progress.total }}
      </p>
      <p v-if="message" class="result">{{ message }}</p>
      <p v-for="line in detail" :key="line" class="stranded-note">{{ line }}</p>
    </div>

    <button class="btn btn-secondary sign-out" type="button" @click="signOut">Log out</button>
  </div>
</template>

<style scoped>
.stack { display: flex; flex-direction: column; gap: 28px; max-width: 440px; }
.spaced { margin-top: 10px; }
.progress,
.result { font-size: 12px; color: var(--color-accent-700); margin: 8px 0 0; }
.sign-out { align-self: flex-start; }
</style>
