import { createI18n } from 'vue-i18n'

type LocaleCode = 'en' | 'zh' | 'ko' | 'ja' | 'vi' | 'zh-TW'

type LocaleMessages = Record<string, any>

const LOCALE_KEY = 'sub2api_locale'
const DEFAULT_LOCALE: LocaleCode = 'en'

const localeLoaders: Record<LocaleCode, () => Promise<{ default: LocaleMessages }>> = {
  en: () => import('./locales/en'),
  zh: () => import('./locales/zh'),
  ko: () => import('./locales/ko'),
  ja: () => import('./locales/ja'),
  vi: () => import('./locales/vi'),
  'zh-TW': () => import('./locales/zh-TW')
}

function isLocaleCode(value: string): value is LocaleCode {
  return (
    value === 'en' ||
    value === 'zh' ||
    value === 'ko' ||
    value === 'ja' ||
    value === 'vi' ||
    value === 'zh-TW'
  )
}

function getDefaultLocale(): LocaleCode {
  const saved = localStorage.getItem(LOCALE_KEY)
  if (saved && isLocaleCode(saved)) {
    return saved
  }

  const browserLang = navigator.language
  const lower = browserLang.toLowerCase()

  // Traditional Chinese variants (Taiwan, Hong Kong, Macau)
  if (lower === 'zh-tw' || lower === 'zh-hk' || lower === 'zh-mo' || lower.startsWith('zh-hant')) {
    return 'zh-TW'
  }
  // Simplified Chinese and generic zh
  if (lower.startsWith('zh')) {
    return 'zh'
  }
  if (lower.startsWith('ko')) {
    return 'ko'
  }
  if (lower.startsWith('ja')) {
    return 'ja'
  }
  if (lower.startsWith('vi')) {
    return 'vi'
  }

  return DEFAULT_LOCALE
}

export const i18n = createI18n({
  legacy: false,
  locale: getDefaultLocale(),
  fallbackLocale: DEFAULT_LOCALE,
  messages: {},
  // 禁用 HTML 消息警告 - 引导步骤使用富文本内容（driver.js 支持 HTML）
  // 这些内容是内部定义的，不存在 XSS 风险
  warnHtmlMessage: false
})

const loadedLocales = new Set<LocaleCode>()

export async function loadLocaleMessages(locale: LocaleCode): Promise<void> {
  if (loadedLocales.has(locale)) {
    return
  }

  const loader = localeLoaders[locale]
  const module = await loader()
  i18n.global.setLocaleMessage(locale, module.default)
  loadedLocales.add(locale)
}

export async function initI18n(): Promise<void> {
  const current = getLocale()
  // Always load the default locale so fallback messages are available
  if (current !== DEFAULT_LOCALE) {
    await loadLocaleMessages(DEFAULT_LOCALE)
  }
  await loadLocaleMessages(current)
  document.documentElement.setAttribute('lang', current)
}

export async function setLocale(locale: string): Promise<void> {
  if (!isLocaleCode(locale)) {
    return
  }

  // Ensure fallback (English) messages are loaded so missing keys fall back cleanly
  if (locale !== DEFAULT_LOCALE) {
    await loadLocaleMessages(DEFAULT_LOCALE)
  }
  await loadLocaleMessages(locale)
  i18n.global.locale.value = locale
  localStorage.setItem(LOCALE_KEY, locale)
  document.documentElement.setAttribute('lang', locale)

  // 同步更新浏览器页签标题，使其跟随语言切换
  const { resolveDocumentTitle } = await import('@/router/title')
  const { default: router } = await import('@/router')
  const { useAppStore } = await import('@/stores/app')
  const route = router.currentRoute.value
  const appStore = useAppStore()
  document.title = resolveDocumentTitle(route.meta.title, appStore.siteName, route.meta.titleKey as string)
}

export function getLocale(): LocaleCode {
  const current = i18n.global.locale.value
  return isLocaleCode(current) ? current : DEFAULT_LOCALE
}

export const availableLocales = [
  { code: 'en', name: 'English', flag: '🇺🇸' },
  { code: 'zh', name: '中文', flag: '🇨🇳' },
  { code: 'zh-TW', name: '繁體中文', flag: '🇹🇼' },
  { code: 'ja', name: '日本語', flag: '🇯🇵' },
  { code: 'ko', name: '한국어', flag: '🇰🇷' },
  { code: 'vi', name: 'Tiếng Việt', flag: '🇻🇳' }
] as const

export default i18n
