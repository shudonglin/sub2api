<template>
  <button
    @click="toggleLocale"
    :disabled="switching"
    class="relative flex h-9 items-center justify-center rounded-lg border border-gray-200 bg-white px-2.5 text-xs font-medium text-gray-600 transition-colors hover:text-gray-900 disabled:cursor-not-allowed disabled:opacity-60 dark:border-dark-700 dark:bg-dark-800 dark:text-dark-300 dark:hover:text-white"
    :aria-label="`Switch language to ${locale === 'en' ? 'Chinese' : 'English'}`"
  >
    {{ locale === 'en' ? '中文' : 'EN' }}
  </button>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { setLocale } from '@/i18n'

const { locale } = useI18n()
const switching = ref(false)

async function toggleLocale() {
  if (switching.value) return
  switching.value = true
  try {
    await setLocale(locale.value === 'en' ? 'zh' : 'en')
  } finally {
    switching.value = false
  }
}
</script>
