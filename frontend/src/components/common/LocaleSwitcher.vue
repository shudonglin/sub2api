<template>
  <div class="relative" ref="rootRef">
    <button
      @click="toggleOpen"
      :disabled="switching"
      type="button"
      class="relative flex h-9 min-w-[2.5rem] items-center justify-center gap-1 rounded-lg border border-gray-200 bg-white px-2.5 text-xs font-medium text-gray-600 transition-colors hover:text-gray-900 disabled:cursor-not-allowed disabled:opacity-60 dark:border-dark-700 dark:bg-dark-800 dark:text-dark-300 dark:hover:text-white"
      :aria-label="`Current language: ${currentName}. Click to change.`"
      :aria-expanded="open"
      aria-haspopup="listbox"
    >
      <span>{{ currentShortLabel }}</span>
      <svg
        width="10"
        height="10"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2.5"
        stroke-linecap="round"
        stroke-linejoin="round"
        class="opacity-60"
        :class="{ 'rotate-180': open }"
      >
        <polyline points="6 9 12 15 18 9" />
      </svg>
    </button>

    <Transition
      enter-active-class="transition ease-out duration-100"
      enter-from-class="opacity-0 scale-95"
      enter-to-class="opacity-100 scale-100"
      leave-active-class="transition ease-in duration-75"
      leave-from-class="opacity-100 scale-100"
      leave-to-class="opacity-0 scale-95"
    >
      <div
        v-if="open"
        role="listbox"
        class="absolute right-0 top-full z-50 mt-2 min-w-[10rem] origin-top-right overflow-hidden rounded-lg border border-gray-200 bg-white py-1 shadow-lg dark:border-dark-700 dark:bg-dark-800"
      >
        <button
          v-for="item in availableLocales"
          :key="item.code"
          type="button"
          role="option"
          :aria-selected="item.code === locale"
          @click="selectLocale(item.code)"
          class="flex w-full items-center gap-2.5 px-3 py-2 text-left text-xs font-medium transition-colors hover:bg-gray-100 dark:hover:bg-dark-700"
          :class="
            item.code === locale
              ? 'text-gray-900 dark:text-white'
              : 'text-gray-600 dark:text-dark-300'
          "
        >
          <span aria-hidden="true" class="text-base leading-none">{{ item.flag }}</span>
          <span class="flex-1">{{ item.name }}</span>
          <svg
            v-if="item.code === locale"
            width="14"
            height="14"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2.5"
            stroke-linecap="round"
            stroke-linejoin="round"
            class="text-brand-500"
          >
            <polyline points="20 6 9 17 4 12" />
          </svg>
        </button>
      </div>
    </Transition>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { availableLocales, setLocale } from '@/i18n'

const { locale } = useI18n()
const switching = ref(false)
const open = ref(false)
const rootRef = ref<HTMLElement | null>(null)

const shortLabelMap: Record<string, string> = {
  en: 'EN',
  zh: '中',
  'zh-TW': '繁',
  ja: '日',
  ko: '한',
  vi: 'VN'
}

const currentShortLabel = computed(() => {
  const code = String(locale.value)
  return shortLabelMap[code] ?? code.slice(0, 2).toUpperCase()
})

const currentName = computed(() => {
  const code = String(locale.value)
  const match = availableLocales.find((item) => item.code === code)
  return match?.name ?? code
})

function toggleOpen() {
  if (switching.value) return
  open.value = !open.value
}

async function selectLocale(code: string) {
  if (code === locale.value) {
    open.value = false
    return
  }
  if (switching.value) return
  switching.value = true
  try {
    await setLocale(code)
  } finally {
    switching.value = false
    open.value = false
  }
}

function handleClickOutside(event: MouseEvent) {
  if (!open.value) return
  const target = event.target as Node | null
  if (rootRef.value && target && !rootRef.value.contains(target)) {
    open.value = false
  }
}

function handleKeyDown(event: KeyboardEvent) {
  if (event.key === 'Escape' && open.value) {
    open.value = false
  }
}

onMounted(() => {
  document.addEventListener('click', handleClickOutside)
  document.addEventListener('keydown', handleKeyDown)
})

onBeforeUnmount(() => {
  document.removeEventListener('click', handleClickOutside)
  document.removeEventListener('keydown', handleKeyDown)
})
</script>
