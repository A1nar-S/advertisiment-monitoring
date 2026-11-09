import { computed, ref } from 'vue'
import en, { type Messages, type Plural } from './en'
import lv from './lv'

// A small translation layer: English and Latvian, plural forms through
// Intl.PluralRules, numbers and dates in the chosen language. The choice is
// kept per browser.

export type Locale = 'en' | 'lv'
export const locales: { id: Locale; label: string }[] = [
  { id: 'en', label: 'English' },
  { id: 'lv', label: 'Latviešu' },
]
const messages: Record<Locale, Messages> = { en, lv }
const storageKey = 'locale'

function initial(): Locale {
  try {
    const saved = localStorage.getItem(storageKey)
    if (saved === 'en' || saved === 'lv') return saved
  } catch {
    // Storage can be blocked; fall back to the browser's language.
  }
  return navigator.language.toLowerCase().startsWith('lv') ? 'lv' : 'en'
}

export const locale = ref<Locale>(initial())
document.documentElement.lang = locale.value

export function setLocale(next: Locale) {
  locale.value = next
  document.documentElement.lang = next
  try {
    localStorage.setItem(storageKey, next)
  } catch {
    // Not remembered, but still applied.
  }
}

// The Intl locale for numbers and dates: en-GB writes "9 Nov 2026, 14:32",
// which reads naturally next to Latvian ads.
export const intlLocale = computed(() =>
  locale.value === 'lv' ? 'lv-LV' : 'en-GB',
)

// "a.b.c" for every message in the English source.
type Keys<T, P extends string = ''> = {
  [K in keyof T & string]: T[K] extends string | Plural
    ? `${P}${K}`
    : Keys<T[K], `${P}${K}.`>
}[keyof T & string]
export type MessageKey = Keys<Messages>

type Params = Record<string, string | number>

function lookup(source: Messages, key: string): string | Plural | undefined {
  let node: unknown = source
  for (const part of key.split('.')) {
    if (node === null || typeof node !== 'object') return undefined
    node = (node as Record<string, unknown>)[part]
  }
  return typeof node === 'string' || (node && typeof node === 'object')
    ? (node as string | Plural)
    : undefined
}

// t translates key. A plural message picks its form from params.n; numbers
// in params are formatted for the language ({n} → "1 234").
export function t(key: MessageKey, params: Params = {}): string {
  return translate(key, params) ?? key
}

// tKey translates a key built at run time, or returns fallback.
export function tKey(key: string, fallback: string, params: Params = {}) {
  return translate(key, params) ?? fallback
}

function translate(key: string, params: Params): string | undefined {
  let message = lookup(messages[locale.value], key) ?? lookup(messages.en, key)
  if (message === undefined) return undefined
  if (typeof message === 'object') {
    // A group of messages, not a plural: the key is incomplete.
    if (typeof message.other !== 'string') return undefined
    const n = Number(params.n ?? 0)
    const form = new Intl.PluralRules(intlLocale.value).select(n)
    message =
      (message as Record<string, string | undefined>)[form] ?? message.other
  }
  return message.replace(/\{(\w+)\}/g, (match, name: string) => {
    const value = params[name]
    if (value === undefined) return match
    return typeof value === 'number' ? formatNumber(value) : value
  })
}

export function formatNumber(
  value: number,
  options?: Intl.NumberFormatOptions,
): string {
  return value.toLocaleString(intlLocale.value, options)
}

export function formatDate(
  value: string | number | Date,
  options: Intl.DateTimeFormatOptions,
): string {
  return new Date(value).toLocaleString(intlLocale.value, options)
}

// Labels for the API's own words: a search field ("make") or category.
export const fieldLabel = (key: string, fallback = key) =>
  tKey(`fields.${key}`, fallback)
export const categoryName = (id: string, fallback = id) =>
  tKey(`categories.${id}`, fallback)
