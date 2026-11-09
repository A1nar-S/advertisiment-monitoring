<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import {
  ArrowUpRight,
  ChevronLeft,
  ChevronRight,
  CircleAlert,
  Database,
  ImageOff,
  TriangleAlert,
  Layers3,
  RefreshCw,
  Search,
  SlidersHorizontal,
  X,
} from 'lucide-vue-next'
import BaseDialog from './BaseDialog.vue'
import EmptyState from './EmptyState.vue'
import PageHeader from './PageHeader.vue'
import { ApiError, readListingDetails } from '../api'
import { categoryName, formatDate, formatNumber, t, tKey } from '../i18n'

type Listing = {
  id: number
  source: string
  title: string
  url: string
  location: string
  price: string
  imageUrl: string
  active: boolean
  firstSeenAt: string
  attrs: Record<string, unknown>
  // When the ad's own page was last read (region, gearbox, ...); null = never.
  detailedAt?: string | null
}
type SavedSearch = {
  id: number
  name: string
  userName: string
  source: string
  enabled: boolean
  feeds: string[]
  filters: Record<string, unknown>
  channel: string
  matches: number
}
type Overview = {
  updatedAt: string
  stats: {
    listings: number
    activeListings: number
    newToday: number
    searches: number
    enabledSearches: number
    users: number
    matches: number
    deliveries: number
  }
  listings: Listing[]
  searches: SavedSearch[]
}

const data = ref<Overview | null>(null)
const loading = ref(false)
const error = ref('')
const view = ref<'listings' | 'searches'>('listings')
const query = ref('')
const source = ref('all')
const category = ref<'all' | 'cars' | 'flats'>('all')
const categoryOptions = [
  { value: 'all', label: () => t('overview.all') },
  { value: 'cars', label: () => categoryName('cars') },
  { value: 'flats', label: () => categoryName('flats') },
] as const
const status = ref('all')
const sort = ref('newest')
const page = ref(1)
const openListing = ref<Listing | null>(null)
const openSearch = ref<SavedSearch | null>(null)
const failedImages = ref(new Set<number>())
const pageSize = 20
let timer: ReturnType<typeof setInterval> | undefined
let controller: AbortController | undefined

async function refresh() {
  if (loading.value) return
  loading.value = true
  error.value = ''
  controller = new AbortController()
  const timeout = setTimeout(() => controller?.abort(), 10000)
  try {
    const response = await fetch('/api/overview', { signal: controller.signal })
    if (response.status === 403) throw new Error(t('overview.adminsOnly'))
    if (!response.ok) throw new Error(t('overview.dbDown'))
    data.value = await response.json()
  } catch (reason) {
    error.value =
      reason instanceof Error ? reason.message : t('overview.loadFailed')
  } finally {
    clearTimeout(timeout)
    loading.value = false
  }
}

const loadedRecords = computed(() =>
  view.value === 'listings'
    ? (data.value?.listings ?? [])
    : (data.value?.searches ?? []),
)
const sources = computed(() =>
  [...new Set(loadedRecords.value.map((item) => item.source))].sort(),
)
const filteredListings = computed(() =>
  (data.value?.listings ?? [])
    .filter(
      (item) =>
        `${item.title} ${item.location} ${item.source}`
          .toLowerCase()
          .includes(query.value.toLowerCase()) &&
        (source.value === 'all' || item.source === source.value) &&
        (category.value === 'all' ||
          listingCategory(item) === category.value) &&
        (status.value === 'all' || item.active === (status.value === 'active')),
    )
    .sort((first, second) =>
      sort.value === 'title'
        ? first.title.localeCompare(second.title)
        : (Date.parse(second.firstSeenAt) - Date.parse(first.firstSeenAt)) *
          (sort.value === 'oldest' ? -1 : 1),
    ),
)
const filteredSearches = computed(() =>
  (data.value?.searches ?? []).filter(
    (item) =>
      `${item.name} ${item.userName} ${item.source}`
        .toLowerCase()
        .includes(query.value.toLowerCase()) &&
      (source.value === 'all' || item.source === source.value) &&
      (status.value === 'all' || item.enabled === (status.value === 'active')),
  ),
)
const resultCount = computed(() =>
  view.value === 'listings'
    ? filteredListings.value.length
    : filteredSearches.value.length,
)
const hasFilters = computed(
  () =>
    query.value !== '' ||
    source.value !== 'all' ||
    status.value !== 'all' ||
    category.value !== 'all',
)
const pageCount = computed(() =>
  Math.max(1, Math.ceil(resultCount.value / pageSize)),
)
const visibleListings = computed(() =>
  filteredListings.value.slice(
    (page.value - 1) * pageSize,
    page.value * pageSize,
  ),
)
const visibleSearches = computed(() =>
  filteredSearches.value.slice(
    (page.value - 1) * pageSize,
    page.value * pageSize,
  ),
)
const metrics = computed(() => {
  const stats = data.value?.stats
  return [
    {
      name: t('overview.activeListings'),
      value: stats?.activeListings,
      detail: t('overview.ofCollected', { n: stats?.listings ?? 0 }),
    },
    {
      name: t('overview.newToday'),
      value: stats?.newToday,
      detail: t('overview.sinceMidnight'),
    },
    {
      name: t('overview.searchMatches'),
      value: stats?.matches,
      detail: t('overview.searchesActive', {
        active: stats?.enabledSearches ?? 0,
        total: stats?.searches ?? 0,
      }),
    },
    {
      name: t('overview.notificationsSent'),
      value: stats?.deliveries,
      detail: t('overview.acrossUsers', { n: stats?.users ?? 0 }),
    },
  ]
})
const updatedAt = computed(() =>
  data.value
    ? formatDate(data.value.updatedAt, {
        hour: '2-digit',
        minute: '2-digit',
      })
    : '',
)
watch([query, source, category, status, sort, view], () => {
  page.value = 1
})
watch(pageCount, (value) => {
  page.value = Math.min(page.value, value)
})

function listingCategory(listing: Listing): 'cars' | 'flats' | undefined {
  if (listing.source !== 'ss.com') return undefined
  if (
    listing.location === 'transport/cars' ||
    listing.location.startsWith('transport/cars/')
  )
    return 'cars'
  if (
    listing.location === 'real-estate/flats' ||
    listing.location.startsWith('real-estate/flats/')
  )
    return 'flats'
  return undefined
}

// The order specifications are shown in; anything else follows.
const attributeOrder = [
  'make',
  'model',
  'year',
  'mileage_km',
  'engine_l',
  'fuel',
  'gearbox',
  'body',
  'color',
  'seats',
  'region',
  'inspection',
  'rooms',
  'area_m2',
  'floor',
  'floors',
  'series',
  'house_type',
  'district',
  'address',
]
const humanize = (key: string) =>
  tKey(
    `attrs.${key}`,
    key.replace(/_/g, ' ').replace(/^./, (letter) => letter.toUpperCase()),
  )
// Shown in their own sections, not as specifications.
const ownSection = new Set(['text', 'description', 'equipment'])
const rank = (key: string) =>
  attributeOrder.includes(key) ? attributeOrder.indexOf(key) : 999

const listingAttributes = computed(() =>
  Object.entries(openListing.value?.attrs ?? {})
    .filter(
      ([key, value]) => !ownSection.has(key) && value !== null && value !== '',
    )
    .sort(([a], [b]) => rank(a) - rank(b) || a.localeCompare(b))
    .map(([key, value]) => ({
      key,
      label: humanize(key),
      value: formatAttribute(key, value),
    })),
)
const equipment = computed(() => {
  const value = openListing.value?.attrs?.equipment
  return Array.isArray(value) ? value.map(String) : []
})
// The ad's own text: "text" from the ad page (older rows: "description").
const description = computed(() => {
  const attrs = openListing.value?.attrs ?? {}
  const value = attrs.text ?? attrs.description
  return typeof value === 'string' && value.trim() ? value : null
})

// Reading the open listing's ad page on ss.com, which adds what the list
// page lacks (a car's region, gearbox, ...). Done once per ad, then saved.
const details = ref<'idle' | 'loading' | 'failed' | 'gone'>('idle')
const detailsError = ref('')
const hasAdPage = (l: Listing) =>
  l.source === 'ss.com' &&
  (l.location.startsWith('transport/cars') ||
    l.location.startsWith('real-estate/'))

async function loadDetails(listing: Listing) {
  details.value = 'loading'
  detailsError.value = ''
  try {
    const read = await readListingDetails(listing.id)
    listing.attrs = read.attrs
    listing.active = read.active
    listing.detailedAt = read.detailedAt
    if (openListing.value?.id === listing.id) details.value = 'idle'
  } catch (reason) {
    if (openListing.value?.id !== listing.id) return
    if (reason instanceof ApiError && reason.status === 410) {
      listing.active = false
      details.value = 'gone'
      return
    }
    details.value = 'failed'
    detailsError.value =
      reason instanceof ApiError ? reason.message : t('overview.readFailed')
  }
}

watch(
  () => openListing.value?.id,
  () => {
    details.value = 'idle'
    const l = openListing.value
    if (l && !l.detailedAt && hasAdPage(l)) void loadDetails(l)
  },
)

function formatAttribute(key: string, value: unknown): string {
  if (typeof value === 'number') {
    const formatted = formatNumber(value, {
      useGrouping: key !== 'year',
      maximumFractionDigits: 2,
      minimumFractionDigits: key === 'engine_l' ? 1 : 0,
    })
    return `${formatted}${({ mileage_km: ' km', engine_l: ' L', area_m2: ' m²' } as Record<string, string>)[key] ?? ''}`
  }
  if (key === 'fuel' && typeof value === 'string')
    return tKey(`fuelCodes.${value}`, value)
  if (typeof value === 'boolean')
    return value ? t('common.yes') : t('common.no')
  if (Array.isArray(value)) return value.join(', ')
  if (typeof value === 'object') return JSON.stringify(value)
  return String(value)
}

// A saved search's filters as readable rows: lists joined, ranges as a–b.
// Stored searches hold them compiled ({ranges: {...}, one_of: {...}}), so
// those groups are opened up first.
const compiledGroups = ['ranges', 'one_of', 'all_of', 'contains']
function filterRows(filters: Record<string, unknown>) {
  const flat: [string, unknown][] = []
  for (const [key, value] of Object.entries(filters ?? {})) {
    if (compiledGroups.includes(key) && value && typeof value === 'object')
      flat.push(...Object.entries(value as Record<string, unknown>))
    else flat.push([key, value])
  }
  return flat.map(([key, value]) => {
    const n = (v: number) => formatNumber(v, { useGrouping: key !== 'year' })
    let text: string
    if (Array.isArray(value)) text = value.join(', ')
    else if (value && typeof value === 'object') {
      const { min, max } = value as { min?: number; max?: number }
      text =
        min !== undefined && max !== undefined
          ? `${n(min)} – ${n(max)}`
          : min !== undefined
            ? t('overview.orMore', { n: n(min) })
            : max !== undefined
              ? t('overview.upTo', { n: n(max) })
              : JSON.stringify(value)
    } else text = String(value)
    return { key, label: humanize(key), value: text }
  })
}

function safeURL(value: string) {
  try {
    const url = new URL(value)
    return ['https:', 'http:'].includes(url.protocol) ? url.href : undefined
  } catch {
    return undefined
  }
}
function formatTime(value: string) {
  return formatDate(value, { dateStyle: 'medium', timeStyle: 'short' })
}
function resetFilters() {
  query.value = ''
  source.value = 'all'
  category.value = 'all'
  status.value = 'all'
}
function changeView(next: 'listings' | 'searches') {
  view.value = next
  resetFilters()
}

onMounted(() => {
  void refresh()
  timer = setInterval(() => void refresh(), 30000)
})
onUnmounted(() => {
  clearInterval(timer)
  controller?.abort()
})
</script>

<template>
  <section>
    <PageHeader :title="t('overview.title')" :description="t('overview.lead')">
      <span
        v-if="data"
        class="mr-2 hidden text-small text-gray-500 tabular-nums sm:inline"
        >{{ t('overview.updated', { time: updatedAt }) }}</span
      >
      <button class="btn-secondary" :disabled="loading" @click="refresh">
        <RefreshCw :size="14" :class="{ 'animate-spin': loading }" />{{
          t('overview.refresh')
        }}
      </button>
    </PageHeader>

    <div v-if="error" role="alert" class="message-bar-danger mb-6">
      <CircleAlert :size="16" class="mt-0.5 shrink-0 text-danger-600" />
      <span class="flex-1"
        >{{ error
        }}<template v-if="data"> {{ t('overview.staleData') }}</template></span
      >
    </div>

    <section
      :aria-label="t('overview.totals')"
      class="grid grid-cols-2 gap-px overflow-hidden rounded-lg bg-gray-150 shadow-card md:grid-cols-4"
    >
      <div v-for="metric in metrics" :key="metric.name" class="bg-surface p-5">
        <p class="text-small text-gray-600">{{ metric.name }}</p>
        <p
          v-if="metric.value !== undefined"
          class="mt-1 font-display text-display font-semibold tracking-[-0.015em] text-gray-900 tabular-nums"
        >
          {{ formatNumber(metric.value) }}
        </p>
        <div v-else class="skeleton mt-2 mb-1 h-7 w-16" />
        <p class="mt-1 truncate text-xs text-gray-500">{{ metric.detail }}</p>
      </div>
    </section>

    <div
      class="mt-10 flex gap-6 border-b border-gray-200"
      role="tablist"
      :aria-label="t('overview.records')"
    >
      <button
        v-for="item in [
          {
            id: 'listings' as const,
            label: t('overview.tabListings'),
            count: data?.stats.listings,
          },
          {
            id: 'searches' as const,
            label: t('overview.tabSearches'),
            count: data?.stats.searches,
          },
        ]"
        :key="item.id"
        role="tab"
        :aria-selected="view === item.id"
        class="relative -mb-px flex h-10 items-center gap-2 text-sm transition-colors"
        :class="
          view === item.id
            ? 'font-medium text-gray-900'
            : 'text-gray-600 hover:text-gray-900'
        "
        @click="changeView(item.id)"
      >
        {{ item.label }}
        <span
          v-if="item.count !== undefined"
          class="text-small font-normal text-gray-500 tabular-nums"
          >{{ formatNumber(item.count) }}</span
        >
        <span
          v-if="view === item.id"
          class="absolute inset-x-0 bottom-0 h-[3px] rounded-full bg-brand-600"
          aria-hidden="true"
        />
      </button>
    </div>

    <div
      v-if="loadedRecords.length"
      class="mt-5 mb-4 flex flex-wrap items-center gap-2"
    >
      <label
        class="field-within flex h-8 w-full items-center gap-2 pr-1 pl-2.5 sm:w-72"
      >
        <Search :size="15" class="shrink-0 text-gray-400" />
        <input
          v-model="query"
          class="h-full w-full min-w-0 bg-transparent text-sm outline-none placeholder:text-gray-400 focus-visible:outline-none"
          :placeholder="
            view === 'listings'
              ? t('overview.searchListings')
              : t('overview.searchSearches')
          "
          :aria-label="t('overview.searchRecords')"
        />
        <button
          v-if="query"
          class="grid size-6 shrink-0 place-items-center rounded text-gray-500 hover:bg-gray-100 hover:text-gray-900"
          :aria-label="t('overview.clearSearch')"
          :title="t('overview.clearSearch')"
          @click.prevent="query = ''"
        >
          <X :size="14" />
        </button>
      </label>

      <div
        v-if="view === 'listings'"
        class="flex rounded-md bg-gray-150/80 p-0.5"
        role="group"
        :aria-label="t('overview.category')"
      >
        <button
          v-for="option in categoryOptions"
          :key="option.value"
          :aria-pressed="category === option.value"
          class="h-7 rounded-[5px] px-3 text-small transition-colors"
          :class="
            category === option.value
              ? 'bg-raised font-medium text-gray-900 shadow-card'
              : 'text-gray-600 hover:text-gray-900'
          "
          @click="category = option.value"
        >
          {{ option.label() }}
        </button>
      </div>

      <select
        v-model="status"
        class="field w-auto"
        :aria-label="t('overview.status')"
      >
        <option value="all">{{ t('overview.anyStatus') }}</option>
        <option value="active">
          {{ view === 'listings' ? t('common.active') : t('common.enabled') }}
        </option>
        <option value="inactive">
          {{ view === 'listings' ? t('common.inactive') : t('common.paused') }}
        </option>
      </select>
      <select
        v-if="sources.length > 1"
        v-model="source"
        class="field w-auto"
        :aria-label="t('overview.source')"
      >
        <option value="all">{{ t('overview.allSources') }}</option>
        <option v-for="item in sources" :key="item" :value="item">
          {{ item }}
        </option>
      </select>
      <button v-if="hasFilters" class="btn-subtle" @click="resetFilters">
        {{ t('overview.clearFilters') }}
      </button>
      <select
        v-if="view === 'listings'"
        v-model="sort"
        class="field w-auto sm:ml-auto"
        :aria-label="t('overview.sort')"
      >
        <option value="newest">{{ t('overview.newest') }}</option>
        <option value="oldest">{{ t('overview.oldest') }}</option>
        <option value="title">{{ t('overview.byTitle') }}</option>
      </select>
    </div>
    <div v-else class="mt-5" />

    <div class="card overflow-hidden" :aria-busy="loading">
      <div
        v-if="!data && loading"
        class="divide-y divide-gray-100"
        role="status"
        :aria-label="t('common.loading')"
      >
        <div v-for="n in 5" :key="n" class="flex items-center gap-4 px-4 py-3">
          <div class="skeleton h-9 w-12 rounded-md" />
          <div class="flex-1">
            <div class="skeleton h-4 w-80 max-w-full" />
            <div class="skeleton mt-2 h-3 w-40" />
          </div>
          <div class="skeleton h-4 w-16" />
        </div>
      </div>

      <EmptyState
        v-else-if="!data && error"
        :icon="Database"
        :title="t('overview.unavailableTitle')"
        :description="t('overview.unavailableBody')"
      >
        <button class="btn-secondary" @click="refresh">
          <RefreshCw :size="14" />{{ t('common.tryAgain') }}
        </button>
      </EmptyState>

      <EmptyState
        v-else-if="resultCount === 0"
        :icon="
          hasFilters
            ? Search
            : view === 'listings'
              ? Layers3
              : SlidersHorizontal
        "
        :title="
          hasFilters
            ? t('overview.noMatchTitle')
            : view === 'listings'
              ? t('overview.noListingsTitle')
              : t('overview.noSearchesTitle')
        "
        :description="
          hasFilters
            ? t('overview.noMatchBody')
            : view === 'listings'
              ? t('overview.noListingsBody')
              : t('overview.noSearchesBody')
        "
      >
        <button v-if="hasFilters" class="btn-secondary" @click="resetFilters">
          {{ t('overview.clearFilters') }}
        </button>
      </EmptyState>

      <div v-else-if="view === 'listings'" class="overflow-x-auto">
        <table class="w-full min-w-[720px] border-collapse">
          <thead class="border-b border-gray-150 bg-gray-25">
            <tr>
              <th class="table-head w-1/2">{{ t('overview.colListing') }}</th>
              <th class="table-head text-right">
                {{ t('overview.colPrice') }}
              </th>
              <th class="table-head">{{ t('overview.colStatus') }}</th>
              <th class="table-head">{{ t('overview.colFirstSeen') }}</th>
              <th class="table-head w-12">
                <span class="sr-only">{{ t('overview.colDetails') }}</span>
              </th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100">
            <tr
              v-for="listing in visibleListings"
              :key="listing.id"
              class="group cursor-pointer transition-colors hover:bg-gray-25"
              @click="openListing = listing"
            >
              <td class="max-w-0 px-4 py-2.5">
                <div class="flex items-center gap-3">
                  <div
                    class="grid h-9 w-12 shrink-0 place-items-center overflow-hidden rounded-md bg-gray-100 text-gray-400"
                  >
                    <img
                      v-if="
                        safeURL(listing.imageUrl) &&
                        !failedImages.has(listing.id)
                      "
                      :src="safeURL(listing.imageUrl)"
                      alt=""
                      class="size-full object-cover"
                      loading="lazy"
                      referrerpolicy="no-referrer"
                      @error="failedImages.add(listing.id)"
                    /><ImageOff v-else :size="15" />
                  </div>
                  <div class="min-w-0">
                    <button
                      class="block max-w-full truncate text-left text-sm text-gray-900 focus-visible:outline-offset-0"
                      @click.stop="openListing = listing"
                    >
                      {{ listing.title }}
                    </button>
                    <p class="mt-0.5 truncate text-xs text-gray-500">
                      {{ listing.source }} ·
                      {{ listing.location || t('overview.noCategory') }}
                    </p>
                  </div>
                </div>
              </td>
              <td
                class="px-4 py-2.5 text-right text-sm font-medium whitespace-nowrap text-gray-900 tabular-nums"
              >
                {{ listing.price || '—' }}
              </td>
              <td class="px-4 py-2.5">
                <span
                  :class="listing.active ? 'badge-success' : 'badge-neutral'"
                  >{{
                    listing.active ? t('common.active') : t('common.inactive')
                  }}</span
                >
              </td>
              <td
                class="px-4 py-2.5 text-small whitespace-nowrap text-gray-600 tabular-nums"
              >
                {{ formatTime(listing.firstSeenAt) }}
              </td>
              <td class="px-4 py-2.5 text-gray-400">
                <ChevronRight
                  :size="16"
                  class="transition-colors group-hover:text-gray-700"
                />
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div v-else class="overflow-x-auto">
        <table class="w-full min-w-[720px] border-collapse">
          <thead class="border-b border-gray-150 bg-gray-25">
            <tr>
              <th class="table-head w-1/2">{{ t('overview.colSearch') }}</th>
              <th class="table-head">{{ t('overview.colSendsTo') }}</th>
              <th class="table-head text-right">
                {{ t('overview.colMatches') }}
              </th>
              <th class="table-head">{{ t('overview.colStatus') }}</th>
              <th class="table-head w-12">
                <span class="sr-only">{{ t('overview.colDetails') }}</span>
              </th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100">
            <tr
              v-for="saved in visibleSearches"
              :key="saved.id"
              class="group cursor-pointer transition-colors hover:bg-gray-25"
              @click="openSearch = saved"
            >
              <td class="max-w-0 px-4 py-3">
                <button
                  class="block max-w-full truncate text-left text-sm text-gray-900 focus-visible:outline-offset-0"
                  @click.stop="openSearch = saved"
                >
                  {{ saved.name }}
                </button>
                <p class="mt-0.5 truncate text-xs text-gray-500">
                  {{ saved.userName }} · {{ saved.source }}
                </p>
              </td>
              <td class="max-w-48 truncate px-4 py-3 text-small text-gray-600">
                {{ saved.channel || '—' }}
              </td>
              <td
                class="px-4 py-3 text-right text-sm text-gray-900 tabular-nums"
              >
                {{ formatNumber(saved.matches) }}
              </td>
              <td class="px-4 py-3">
                <span
                  :class="saved.enabled ? 'badge-success' : 'badge-warning'"
                  >{{
                    saved.enabled ? t('common.enabled') : t('common.paused')
                  }}</span
                >
              </td>
              <td class="px-4 py-3 text-gray-400">
                <ChevronRight
                  :size="16"
                  class="transition-colors group-hover:text-gray-700"
                />
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <footer
        v-if="resultCount > 0"
        class="flex items-center justify-between gap-4 border-t border-gray-150 px-4 py-2 text-small text-gray-500"
      >
        <span class="tabular-nums">{{
          t('overview.range', {
            from: (page - 1) * pageSize + 1,
            to: Math.min(page * pageSize, resultCount),
            total: resultCount,
          })
        }}</span>
        <div v-if="pageCount > 1" class="flex items-center gap-1">
          <span class="mr-2 tabular-nums">{{
            t('overview.page', { page, pages: pageCount })
          }}</span>
          <button
            class="btn-icon"
            :aria-label="t('overview.previousPage')"
            :title="t('overview.previousPage')"
            :disabled="page === 1"
            @click="page--"
          >
            <ChevronLeft :size="16" />
          </button>
          <button
            class="btn-icon"
            :aria-label="t('overview.nextPage')"
            :title="t('overview.nextPage')"
            :disabled="page === pageCount"
            @click="page++"
          >
            <ChevronRight :size="16" />
          </button>
        </div>
      </footer>
    </div>
    <p class="mt-3 text-xs text-gray-500">
      {{ t('overview.footnote') }}
    </p>

    <BaseDialog
      v-if="openListing"
      :title="t('overview.listing')"
      :subtitle="`#${openListing.id} · ${openListing.source}`"
      light-dismiss
      @close="openListing = null"
    >
      <div
        class="grid aspect-[16/10] place-items-center overflow-hidden rounded-lg bg-gray-100 text-gray-400"
      >
        <img
          v-if="
            safeURL(openListing.imageUrl) && !failedImages.has(openListing.id)
          "
          :src="safeURL(openListing.imageUrl)"
          :alt="openListing.title"
          class="size-full object-contain"
          referrerpolicy="no-referrer"
          @error="failedImages.add(openListing.id)"
        /><ImageOff v-else :size="24" />
      </div>

      <div class="mt-5 flex items-start justify-between gap-4">
        <p
          class="font-display text-display font-semibold tracking-[-0.015em] tabular-nums"
        >
          {{ openListing.price || t('overview.noPrice') }}
        </p>
        <span
          class="mt-2"
          :class="openListing.active ? 'badge-success' : 'badge-neutral'"
          >{{
            openListing.active ? t('common.active') : t('common.inactive')
          }}</span
        >
      </div>
      <p class="mt-1 text-sm text-gray-700">{{ openListing.title }}</p>
      <a
        v-if="safeURL(openListing.url)"
        :href="safeURL(openListing.url)"
        target="_blank"
        rel="noopener noreferrer"
        class="btn-primary mt-5 w-full"
        >{{ t('overview.openOn', { source: openListing.source })
        }}<ArrowUpRight :size="16"
      /></a>

      <div
        v-if="details === 'gone'"
        role="status"
        class="message-bar-warning mt-6"
      >
        <TriangleAlert :size="16" class="mt-0.5 shrink-0 text-warning-600" />
        {{ t('overview.gone', { source: openListing.source }) }}
      </div>

      <section class="mt-8" :aria-busy="details === 'loading'">
        <div class="mb-3 flex min-h-6 items-center justify-between gap-3">
          <h3 class="text-sm font-semibold text-gray-900">
            {{ t('overview.specs') }}
          </h3>
          <span
            v-if="details === 'loading'"
            class="flex items-center gap-2 text-xs text-gray-500"
            role="status"
            ><span
              class="size-3.5 animate-spin rounded-full border-2 border-gray-200 border-t-brand-600"
            />{{ t('overview.readingAdPage') }}</span
          >
          <span
            v-else-if="openListing.detailedAt && hasAdPage(openListing)"
            class="flex items-center gap-1 text-xs text-gray-500"
            >{{
              t('overview.fromAdPage', {
                time: formatTime(openListing.detailedAt),
              })
            }}
            <button
              class="btn-icon size-6"
              :aria-label="t('overview.readAgain')"
              :title="t('overview.readAgain')"
              @click="loadDetails(openListing)"
            >
              <RefreshCw :size="13" /></button
          ></span>
        </div>
        <div
          v-if="details === 'failed'"
          role="alert"
          class="message-bar-danger mb-4"
        >
          <CircleAlert :size="16" class="mt-0.5 shrink-0 text-danger-600" />
          <span class="flex-1"
            >{{ detailsError }} {{ t('overview.listPageShown') }}</span
          >
          <button
            class="shrink-0 text-small font-medium text-danger-700 hover:underline"
            @click="loadDetails(openListing)"
          >
            {{ t('common.tryAgain') }}
          </button>
        </div>
        <dl
          v-if="listingAttributes.length"
          class="grid grid-cols-2 gap-x-6 gap-y-4"
        >
          <div v-for="attribute in listingAttributes" :key="attribute.key">
            <dt class="text-xs text-gray-500">{{ attribute.label }}</dt>
            <dd
              class="mt-0.5 text-sm text-gray-900 tabular-nums [overflow-wrap:anywhere]"
            >
              {{ attribute.value }}
            </dd>
          </div>
          <template v-if="details === 'loading'">
            <div v-for="n in 4" :key="n">
              <div class="skeleton h-3 w-16" />
              <div class="skeleton mt-1.5 h-4 w-24" />
            </div>
          </template>
        </dl>
        <p v-else class="text-sm text-gray-500">
          {{ t('overview.noSpecs') }}
        </p>
      </section>

      <section v-if="equipment.length" class="mt-8">
        <h3 class="mb-3 text-sm font-semibold text-gray-900">
          {{ t('overview.equipment') }}
        </h3>
        <ul class="flex flex-wrap gap-1.5">
          <li
            v-for="item in equipment"
            :key="item"
            class="rounded-md bg-gray-100 px-2 py-1 text-small text-gray-800"
          >
            {{ item }}
          </li>
        </ul>
      </section>

      <section class="mt-8">
        <h3 class="mb-2 text-sm font-semibold text-gray-900">
          {{ t('overview.description') }}
        </h3>
        <p
          v-if="description"
          class="text-sm leading-6 whitespace-pre-wrap text-gray-700 [overflow-wrap:anywhere]"
        >
          {{ description }}
        </p>
        <div v-else-if="details === 'loading'" class="grid gap-2">
          <div class="skeleton h-3.5 w-full" />
          <div class="skeleton h-3.5 w-11/12" />
          <div class="skeleton h-3.5 w-2/3" />
        </div>
        <p v-else class="text-sm text-gray-500">
          {{
            openListing.detailedAt
              ? t('overview.noDescription')
              : t('overview.notReadYet')
          }}
        </p>
      </section>

      <section class="mt-8 border-t border-gray-150 pt-6">
        <dl class="grid gap-3 text-sm">
          <div class="flex gap-4">
            <dt class="w-32 shrink-0 text-gray-500">
              {{ t('overview.firstSeen') }}
            </dt>
            <dd class="text-gray-900 tabular-nums">
              {{ formatTime(openListing.firstSeenAt) }}
            </dd>
          </div>
          <div class="flex gap-4">
            <dt class="w-32 shrink-0 text-gray-500">
              {{ t('overview.categoryPath') }}
            </dt>
            <dd
              class="min-w-0 font-mono text-xs leading-5 text-gray-700 [overflow-wrap:anywhere]"
            >
              {{ openListing.location || '—' }}
            </dd>
          </div>
        </dl>
        <details class="group mt-5">
          <summary
            class="flex list-none items-center gap-1 text-small font-medium text-gray-700 hover:text-gray-900 [&::-webkit-details-marker]:hidden"
          >
            <ChevronRight
              :size="14"
              class="transition-transform group-open:rotate-90"
            />{{ t('overview.rawAttributes') }}
          </summary>
          <pre
            class="mt-3 max-h-72 overflow-auto rounded-md bg-gray-50 p-3 font-mono text-xs leading-5 whitespace-pre-wrap text-gray-700 [overflow-wrap:anywhere]"
            >{{ JSON.stringify(openListing.attrs ?? {}, null, 2) }}</pre>
        </details>
      </section>
    </BaseDialog>

    <BaseDialog
      v-if="openSearch"
      :title="openSearch.name"
      :subtitle="t('overview.createdBy', { name: openSearch.userName })"
      light-dismiss
      @close="openSearch = null"
    >
      <dl class="grid gap-3 text-sm">
        <div class="flex gap-4">
          <dt class="w-32 shrink-0 text-gray-500">
            {{ t('overview.colStatus') }}
          </dt>
          <dd>
            <span
              :class="openSearch.enabled ? 'badge-success' : 'badge-warning'"
              >{{
                openSearch.enabled ? t('common.enabled') : t('common.paused')
              }}</span
            >
          </dd>
        </div>
        <div class="flex gap-4">
          <dt class="w-32 shrink-0 text-gray-500">
            {{ t('overview.matches') }}
          </dt>
          <dd class="text-gray-900 tabular-nums">
            {{ formatNumber(openSearch.matches) }}
          </dd>
        </div>
        <div class="flex gap-4">
          <dt class="w-32 shrink-0 text-gray-500">
            {{ t('overview.source') }}
          </dt>
          <dd class="text-gray-900">{{ openSearch.source }}</dd>
        </div>
        <div class="flex gap-4">
          <dt class="w-32 shrink-0 text-gray-500">
            {{ t('overview.sendsTo') }}
          </dt>
          <dd class="min-w-0 text-gray-900 [overflow-wrap:anywhere]">
            {{ openSearch.channel || '—' }}
          </dd>
        </div>
      </dl>

      <section class="mt-8">
        <h3 class="mb-3 text-sm font-semibold text-gray-900">
          {{ t('overview.filters') }}
        </h3>
        <dl
          v-if="filterRows(openSearch.filters).length"
          class="divide-y divide-gray-100 rounded-md shadow-card"
        >
          <div
            v-for="row in filterRows(openSearch.filters)"
            :key="row.key"
            class="flex gap-4 px-3 py-2.5 text-sm"
          >
            <dt class="w-32 shrink-0 text-gray-500">{{ row.label }}</dt>
            <dd class="min-w-0 text-gray-900 [overflow-wrap:anywhere]">
              {{ row.value }}
            </dd>
          </div>
        </dl>
        <p v-else class="text-sm text-gray-500">
          {{ t('overview.noFilters') }}
        </p>
      </section>

      <section class="mt-8">
        <h3 class="mb-1 text-sm font-semibold text-gray-900">
          {{ t('overview.feeds') }}
        </h3>
        <p class="mb-3 text-small text-gray-500">
          {{ t('overview.feedsLead') }}
        </p>
        <ul v-if="openSearch.feeds.length" class="grid gap-1.5">
          <li
            v-for="feed in openSearch.feeds"
            :key="feed"
            class="rounded-md bg-gray-50 px-3 py-2 font-mono text-xs text-gray-700 [overflow-wrap:anywhere]"
          >
            {{ feed }}
          </li>
        </ul>
        <p v-else class="text-sm text-gray-500">{{ t('overview.noFeeds') }}</p>
      </section>
    </BaseDialog>
  </section>
</template>
