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
  { value: 'all', label: 'All' },
  { value: 'cars', label: 'Cars' },
  { value: 'flats', label: 'Flats' },
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
    if (response.status === 403) throw new Error('The overview is for admins.')
    if (!response.ok)
      throw new Error(
        'The database is unavailable. Check that Postgres and the API are running and migrations have been applied.',
      )
    data.value = await response.json()
  } catch (reason) {
    error.value =
      reason instanceof Error
        ? reason.message
        : 'Unable to load dashboard data.'
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
      name: 'Active listings',
      value: stats?.activeListings,
      detail: `of ${(stats?.listings ?? 0).toLocaleString()} collected`,
    },
    {
      name: 'New today',
      value: stats?.newToday,
      detail: 'Since 00:00 UTC',
    },
    {
      name: 'Search matches',
      value: stats?.matches,
      detail: `${stats?.enabledSearches ?? 0} of ${stats?.searches ?? 0} searches active`,
    },
    {
      name: 'Notifications sent',
      value: stats?.deliveries,
      detail: `Across ${stats?.users ?? 0} users`,
    },
  ]
})
const updatedAt = computed(() =>
  data.value
    ? new Date(data.value.updatedAt).toLocaleTimeString([], {
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

// Labels, in the order the specifications are shown; anything else follows.
const attributeLabels: Record<string, string> = {
  make: 'Make',
  model: 'Model',
  year: 'Year',
  mileage_km: 'Mileage',
  engine_l: 'Engine',
  fuel: 'Fuel',
  gearbox: 'Gearbox',
  body: 'Body type',
  color: 'Colour',
  seats: 'Seats',
  region: 'Region',
  inspection: 'Inspection',
  rooms: 'Rooms',
  area_m2: 'Area',
  floor: 'Floor',
  series: 'Building series',
  house_type: 'House type',
  district: 'District',
  address: 'Address',
}
const attributeOrder = Object.keys(attributeLabels)
const humanize = (key: string) =>
  attributeLabels[key] ??
  key.replace(/_/g, ' ').replace(/^./, (letter) => letter.toUpperCase())
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
      reason instanceof ApiError ? reason.message : "Couldn't read the ad page."
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
    const formatted = value.toLocaleString(undefined, {
      useGrouping: key !== 'year',
      maximumFractionDigits: 2,
      minimumFractionDigits: key === 'engine_l' ? 1 : 0,
    })
    return `${formatted}${({ mileage_km: ' km', engine_l: ' L', area_m2: ' m²' } as Record<string, string>)[key] ?? ''}`
  }
  if (key === 'fuel' && typeof value === 'string')
    return (
      (
        { D: 'Diesel', H: 'Hybrid', E: 'Electric', G: 'Gas' } as Record<
          string,
          string
        >
      )[value] ?? value
    )
  if (typeof value === 'boolean') return value ? 'Yes' : 'No'
  if (Array.isArray(value)) return value.join(', ')
  if (typeof value === 'object') return JSON.stringify(value)
  return String(value)
}

// A saved search's filters as readable rows: lists joined, ranges as a–b.
function filterRows(filters: Record<string, unknown>) {
  return Object.entries(filters).map(([key, value]) => {
    const n = (v: number) =>
      v.toLocaleString(undefined, { useGrouping: key !== 'year' })
    let text: string
    if (Array.isArray(value)) text = value.join(', ')
    else if (value && typeof value === 'object') {
      const { min, max } = value as { min?: number; max?: number }
      text =
        min !== undefined && max !== undefined
          ? `${n(min)} – ${n(max)}`
          : min !== undefined
            ? `${n(min)} or more`
            : max !== undefined
              ? `Up to ${n(max)}`
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
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(new Date(value))
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
    <PageHeader
      title="Overview"
      description="Everything the monitor has collected, across all users."
    >
      <span v-if="data" class="mr-2 hidden text-small text-gray-500 sm:inline"
        >Updated <span class="tabular-nums">{{ updatedAt }}</span></span
      >
      <button class="btn-secondary" :disabled="loading" @click="refresh">
        <RefreshCw :size="14" :class="{ 'animate-spin': loading }" />Refresh
      </button>
    </PageHeader>

    <div v-if="error" role="alert" class="message-bar-danger mb-6">
      <CircleAlert :size="16" class="mt-0.5 shrink-0 text-danger-600" />
      <span class="flex-1"
        >{{ error
        }}<template v-if="data"> Showing the last loaded data.</template></span
      >
    </div>

    <section
      aria-label="Totals"
      class="grid grid-cols-2 gap-px overflow-hidden rounded-lg bg-gray-150 shadow-card md:grid-cols-4"
    >
      <div v-for="metric in metrics" :key="metric.name" class="bg-white p-5">
        <p class="text-small text-gray-600">{{ metric.name }}</p>
        <p
          v-if="metric.value !== undefined"
          class="mt-1 font-display text-display font-semibold tracking-[-0.015em] text-gray-900 tabular-nums"
        >
          {{ metric.value.toLocaleString() }}
        </p>
        <div v-else class="skeleton mt-2 mb-1 h-7 w-16" />
        <p class="mt-1 truncate text-xs text-gray-500">{{ metric.detail }}</p>
      </div>
    </section>

    <div
      class="mt-10 flex gap-6 border-b border-gray-200"
      role="tablist"
      aria-label="Records"
    >
      <button
        v-for="item in [
          {
            id: 'listings' as const,
            label: 'Listings',
            count: data?.stats.listings,
          },
          {
            id: 'searches' as const,
            label: 'Saved searches',
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
          >{{ item.count.toLocaleString() }}</span
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
            view === 'listings' ? 'Search listings' : 'Search by name or user'
          "
          aria-label="Search records"
        />
        <button
          v-if="query"
          class="grid size-6 shrink-0 place-items-center rounded text-gray-500 hover:bg-gray-100 hover:text-gray-900"
          aria-label="Clear search"
          title="Clear search"
          @click.prevent="query = ''"
        >
          <X :size="14" />
        </button>
      </label>

      <div
        v-if="view === 'listings'"
        class="flex rounded-md bg-gray-150/80 p-0.5"
        role="group"
        aria-label="Category"
      >
        <button
          v-for="option in categoryOptions"
          :key="option.value"
          :aria-pressed="category === option.value"
          class="h-7 rounded-[5px] px-3 text-small transition-colors"
          :class="
            category === option.value
              ? 'bg-white font-medium text-gray-900 shadow-card'
              : 'text-gray-600 hover:text-gray-900'
          "
          @click="category = option.value"
        >
          {{ option.label }}
        </button>
      </div>

      <select v-model="status" class="field w-auto" aria-label="Status">
        <option value="all">Any status</option>
        <option value="active">
          {{ view === 'listings' ? 'Active' : 'Enabled' }}
        </option>
        <option value="inactive">
          {{ view === 'listings' ? 'Inactive' : 'Paused' }}
        </option>
      </select>
      <select
        v-if="sources.length > 1"
        v-model="source"
        class="field w-auto"
        aria-label="Source"
      >
        <option value="all">All sources</option>
        <option v-for="item in sources" :key="item" :value="item">
          {{ item }}
        </option>
      </select>
      <button v-if="hasFilters" class="btn-subtle" @click="resetFilters">
        Clear filters
      </button>
      <select
        v-if="view === 'listings'"
        v-model="sort"
        class="field w-auto sm:ml-auto"
        aria-label="Sort"
      >
        <option value="newest">Newest first</option>
        <option value="oldest">Oldest first</option>
        <option value="title">Title A–Z</option>
      </select>
    </div>
    <div v-else class="mt-5" />

    <div class="card overflow-hidden" :aria-busy="loading">
      <div
        v-if="!data && loading"
        class="divide-y divide-gray-100"
        role="status"
        aria-label="Loading"
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
        title="Overview unavailable"
        description="The data couldn't be loaded. It refreshes on its own, or you can try now."
      >
        <button class="btn-secondary" @click="refresh">
          <RefreshCw :size="14" />Try again
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
            ? 'No matching records'
            : view === 'listings'
              ? 'No listings yet'
              : 'No saved searches yet'
        "
        :description="
          hasFilters
            ? 'Try a different search or clear the filters.'
            : view === 'listings'
              ? 'Ads appear here as soon as the scraper collects them.'
              : 'Searches appear here once someone creates one.'
        "
      >
        <button v-if="hasFilters" class="btn-secondary" @click="resetFilters">
          Clear filters
        </button>
      </EmptyState>

      <div v-else-if="view === 'listings'" class="overflow-x-auto">
        <table class="w-full min-w-[720px] border-collapse">
          <thead class="border-b border-gray-150 bg-gray-25">
            <tr>
              <th class="table-head w-1/2">Listing</th>
              <th class="table-head text-right">Price</th>
              <th class="table-head">Status</th>
              <th class="table-head">First seen</th>
              <th class="table-head w-12">
                <span class="sr-only">Details</span>
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
                      {{ listing.location || 'No category' }}
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
                  >{{ listing.active ? 'Active' : 'Inactive' }}</span
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
              <th class="table-head w-1/2">Search</th>
              <th class="table-head">Sends to</th>
              <th class="table-head text-right">Matches</th>
              <th class="table-head">Status</th>
              <th class="table-head w-12">
                <span class="sr-only">Details</span>
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
                {{ saved.matches.toLocaleString() }}
              </td>
              <td class="px-4 py-3">
                <span
                  :class="saved.enabled ? 'badge-success' : 'badge-warning'"
                  >{{ saved.enabled ? 'Enabled' : 'Paused' }}</span
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
        <span class="tabular-nums"
          >{{ (page - 1) * pageSize + 1 }}–{{
            Math.min(page * pageSize, resultCount)
          }}
          of {{ resultCount.toLocaleString() }}</span
        >
        <div v-if="pageCount > 1" class="flex items-center gap-1">
          <span class="mr-2 tabular-nums"
            >Page {{ page }} of {{ pageCount }}</span
          >
          <button
            class="btn-icon"
            aria-label="Previous page"
            title="Previous page"
            :disabled="page === 1"
            @click="page--"
          >
            <ChevronLeft :size="16" />
          </button>
          <button
            class="btn-icon"
            aria-label="Next page"
            title="Next page"
            :disabled="page === pageCount"
            @click="page++"
          >
            <ChevronRight :size="16" />
          </button>
        </div>
      </footer>
    </div>
    <p class="mt-3 text-xs text-gray-500">
      Shows the latest 100 records of each kind and refreshes every 30 seconds.
    </p>

    <BaseDialog
      v-if="openListing"
      title="Listing"
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
          {{ openListing.price || 'No price' }}
        </p>
        <span
          class="mt-2"
          :class="openListing.active ? 'badge-success' : 'badge-neutral'"
          >{{ openListing.active ? 'Active' : 'Inactive' }}</span
        >
      </div>
      <p class="mt-1 text-sm text-gray-700">{{ openListing.title }}</p>
      <a
        v-if="safeURL(openListing.url)"
        :href="safeURL(openListing.url)"
        target="_blank"
        rel="noopener noreferrer"
        class="btn-primary mt-5 w-full"
        >Open on {{ openListing.source }}<ArrowUpRight :size="16"
      /></a>

      <div
        v-if="details === 'gone'"
        role="status"
        class="message-bar-warning mt-6"
      >
        <TriangleAlert :size="16" class="mt-0.5 shrink-0 text-warning-600" />
        This ad has been removed from {{ openListing.source }}, so only what was
        collected earlier is shown.
      </div>

      <section class="mt-8" :aria-busy="details === 'loading'">
        <div class="mb-3 flex min-h-6 items-center justify-between gap-3">
          <h3 class="text-sm font-semibold text-gray-900">Specifications</h3>
          <span
            v-if="details === 'loading'"
            class="flex items-center gap-2 text-xs text-gray-500"
            role="status"
            ><span
              class="size-3.5 animate-spin rounded-full border-2 border-gray-200 border-t-brand-600"
            />Reading the ad page…</span
          >
          <span
            v-else-if="openListing.detailedAt && hasAdPage(openListing)"
            class="flex items-center gap-1 text-xs text-gray-500"
            >From the ad page, {{ formatTime(openListing.detailedAt) }}
            <button
              class="btn-icon size-6"
              aria-label="Read the ad page again"
              title="Read the ad page again"
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
            >{{ detailsError }} Showing what the list page had.</span
          >
          <button
            class="shrink-0 text-small font-medium text-danger-700 hover:underline"
            @click="loadDetails(openListing)"
          >
            Try again
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
        <p v-else class="text-sm text-gray-500">No specifications recorded.</p>
      </section>

      <section v-if="equipment.length" class="mt-8">
        <h3 class="mb-3 text-sm font-semibold text-gray-900">Equipment</h3>
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
        <h3 class="mb-2 text-sm font-semibold text-gray-900">Description</h3>
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
              ? 'The ad has no description.'
              : 'Not read from the ad page yet.'
          }}
        </p>
      </section>

      <section class="mt-8 border-t border-gray-150 pt-6">
        <dl class="grid gap-3 text-sm">
          <div class="flex gap-4">
            <dt class="w-32 shrink-0 text-gray-500">First seen</dt>
            <dd class="text-gray-900 tabular-nums">
              {{ formatTime(openListing.firstSeenAt) }}
            </dd>
          </div>
          <div class="flex gap-4">
            <dt class="w-32 shrink-0 text-gray-500">Category path</dt>
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
            />Raw attributes
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
      :subtitle="`Created by ${openSearch.userName}`"
      light-dismiss
      @close="openSearch = null"
    >
      <dl class="grid gap-3 text-sm">
        <div class="flex gap-4">
          <dt class="w-32 shrink-0 text-gray-500">Status</dt>
          <dd>
            <span
              :class="openSearch.enabled ? 'badge-success' : 'badge-warning'"
              >{{ openSearch.enabled ? 'Enabled' : 'Paused' }}</span
            >
          </dd>
        </div>
        <div class="flex gap-4">
          <dt class="w-32 shrink-0 text-gray-500">Matches</dt>
          <dd class="text-gray-900 tabular-nums">
            {{ openSearch.matches.toLocaleString() }}
          </dd>
        </div>
        <div class="flex gap-4">
          <dt class="w-32 shrink-0 text-gray-500">Source</dt>
          <dd class="text-gray-900">{{ openSearch.source }}</dd>
        </div>
        <div class="flex gap-4">
          <dt class="w-32 shrink-0 text-gray-500">Sends to</dt>
          <dd class="min-w-0 text-gray-900 [overflow-wrap:anywhere]">
            {{ openSearch.channel || '—' }}
          </dd>
        </div>
      </dl>

      <section class="mt-8">
        <h3 class="mb-3 text-sm font-semibold text-gray-900">Filters</h3>
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
          No filters, so it matches every new ad.
        </p>
      </section>

      <section class="mt-8">
        <h3 class="mb-1 text-sm font-semibold text-gray-900">Feeds</h3>
        <p class="mb-3 text-small text-gray-500">
          The ss.com pages polled for this search.
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
        <p v-else class="text-sm text-gray-500">No feeds.</p>
      </section>
    </BaseDialog>
  </section>
</template>
