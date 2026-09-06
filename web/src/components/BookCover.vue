<script setup>
import { computed, ref } from 'vue'
import { coverStyle, coverInitial } from '@/lib/covers'

// The prototype had no cover art, so every tile was a tinted letter. The API
// does return cover_url from the metadata provider, so the real cover is used
// when there is one and the letter tile remains the fallback — which is also
// what shows while a slow image loads or after a broken one.
const props = defineProps({
  book: { type: Object, required: true },
  width: { type: String, default: '100%' },
  height: { type: String, required: true },
  fontSize: { type: String, default: '40px' },
})

const failed = ref(false)

const style = computed(() => ({
  ...coverStyle(props.book.id ?? props.book.title),
  width: props.width,
  height: props.height,
  fontSize: props.fontSize,
  flex: props.width === '100%' ? undefined : 'none',
}))

const initial = computed(() => coverInitial(props.book.title))
</script>

<template>
  <div class="cover" :style="style">
    <img
      v-if="book.cover_url && !failed"
      :src="book.cover_url"
      :alt="`Cover of ${book.title}`"
      loading="lazy"
      @error="failed = true"
    />
    <template v-else>{{ initial }}</template>
  </div>
</template>
