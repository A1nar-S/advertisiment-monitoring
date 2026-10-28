<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import {
  Activity,
  ArrowLeft,
  ArrowRight,
  ArrowUpRight,
  Bell,
  Building2,
  CarFront,
  Check,
  ChevronDown,
  ChevronRight,
  CircleAlert,
  Database,
  Globe2,
  ImageOff,
  Layers3,
  Radio,
  RefreshCw,
  Search,
  SearchCheck,
  ShieldCheck,
  Users,
  X,
} from 'lucide-vue-next'

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
  { value: 'all', label: 'All', icon: Layers3 },
  { value: 'cars', label: 'Cars', icon: CarFront },
  { value: 'flats', label: 'Flats', icon: Building2 },
] as const
const status = ref('all')
const sort = ref('newest')
const page = ref(1)
const expanded = ref<number | null>(null)
const expandedListing = ref<number | null>(null)
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
const selectedListing = computed(
  () =>
    visibleListings.value.find(
      (listing) => listing.id === expandedListing.value,
    ) ?? null,
)
const visibleSearches = computed(() =>
  filteredSearches.value.slice(
    (page.value - 1) * pageSize,
    page.value * pageSize,
  ),
)
const metrics = computed(() => [
  {
    name: 'Active listings',
    value: data.value?.stats.activeListings,
    detail: `${data.value?.stats.listings ?? 0} collected in total`,
    icon: Layers3,
  },
  {
    name: 'New today',
    value: data.value?.stats.newToday,
    detail: 'Since 00:00 UTC',
    icon: Activity,
  },
  {
    name: 'Search matches',
    value: data.value?.stats.matches,
    detail: `${data.value?.stats.enabledSearches ?? 0} enabled searches`,
    icon: SearchCheck,
  },
  {
    name: 'Notifications',
    value: data.value?.stats.deliveries,
    detail: 'Recorded deliveries',
    icon: Bell,
  },
])
watch([query, source, category, status, sort, view], () => {
  page.value = 1
})
watch(pageCount, (value) => {
  page.value = Math.min(page.value, value)
})
watch([query, source, category, status, sort, view, page], () => {
  expandedListing.value = null
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

const attributeLabels: Record<string, string> = {
  mileage_km: 'Odometer',
  engine_l: 'Engine size',
  fuel: 'Fuel',
  year: 'Year',
  model: 'Model',
  rooms: 'Rooms',
  area_m2: 'Area',
  floor: 'Floor',
  series: 'Building series',
  district: 'District',
  address: 'Address',
}
const listingAttributes = computed(() =>
  Object.entries(selectedListing.value?.attrs ?? {})
    .filter(
      ([key, value]) => key !== 'description' && value !== null && value !== '',
    )
    .map(([key, value]) => ({
      key,
      label:
        attributeLabels[key] ??
        key.replace(/_/g, ' ').replace(/^./, (letter) => letter.toUpperCase()),
      value: formatAttribute(key, value),
    })),
)
const description = computed(() => {
  const value = selectedListing.value?.attrs?.description
  return typeof value === 'string' && value.trim() ? value : null
})

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
  if (typeof value === 'object') return JSON.stringify(value)
  return String(value)
}
function toggleListing(listing: Listing) {
  expandedListing.value =
    expandedListing.value === listing.id ? null : listing.id
}
function closeListing() {
  const listingID = expandedListing.value
  expandedListing.value = null
  void nextTick(() =>
    document
      .getElementById(`listing-toggle-${listingID}`)
      ?.focus({ preventScroll: true }),
  )
}
function handleEscape(event: KeyboardEvent) {
  if (event.key === 'Escape' && expandedListing.value !== null) {
    event.preventDefault()
    closeListing()
  }
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
  expanded.value = null
  resetFilters()
}

onMounted(() => {
  void refresh()
  timer = setInterval(() => void refresh(), 30000)
  window.addEventListener('keydown', handleEscape)
})
onUnmounted(() => {
  clearInterval(timer)
  controller?.abort()
  window.removeEventListener('keydown', handleEscape)
})
</script>

<template>
  <div class="min-h-screen">
    <header class="border-b border-zinc-200 bg-white">
      <div
        class="mx-auto flex h-18 max-w-[1328px] items-center justify-between gap-4 px-5 sm:px-8 lg:px-12"
      >
        <a
          href="/"
          aria-label="Ad Monitor home"
          class="flex items-center gap-2.5"
          ><span
            class="grid size-8 place-items-center rounded-md bg-accent-600 text-white"
            ><Radio :size="20" :stroke-width="1.8" /></span
          ><span class="text-lg font-semibold"
            >admonitor<span class="text-accent-600">.</span></span
          ></a
        >
        <div class="flex items-center gap-5 text-xs">
          <span class="flex items-center gap-2 text-zinc-600"
            ><span class="size-1.5 rounded-full bg-emerald-600" />Local<span
              class="hidden sm:inline"
              >workspace</span
            ></span
          ><span
            class="hidden items-center gap-1.5 border-l border-zinc-200 pl-5 text-zinc-500 sm:flex"
            ><ShieldCheck :size="14" />Read only</span
          >
        </div>
      </div>
      <nav
        aria-label="Main navigation"
        class="mx-auto flex max-w-[1328px] items-center gap-7 px-5 sm:px-8 lg:px-12"
      >
        <button
          v-for="item in [
            {
              id: 'listings' as const,
              label: 'Listings',
              icon: Layers3,
              count: data?.stats.listings,
            },
            {
              id: 'searches' as const,
              label: 'Saved searches',
              icon: SearchCheck,
              count: data?.stats.searches,
            },
          ]"
          :key="item.id"
          :aria-current="view === item.id ? 'page' : undefined"
          class="-mb-px flex h-12 items-center gap-2 border-b-2 text-sm transition-colors"
          :class="
            view === item.id
              ? 'border-accent-600 font-semibold text-zinc-900'
              : 'border-transparent text-zinc-500 hover:text-zinc-900'
          "
          @click="changeView(item.id)"
        >
          <component :is="item.icon" :size="16" /><span>{{ item.label }}</span
          ><span
            class="rounded bg-zinc-100 px-1.5 py-0.5 font-mono text-[10px] font-normal text-zinc-500"
            >{{ item.count ?? '-' }}</span
          >
        </button>
        <span
          class="ml-auto hidden items-center gap-1.5 text-xs text-zinc-500 md:flex"
          ><Users :size="14" />{{ data?.stats.users ?? '-' }} users</span
        >
      </nav>
    </header>

    <main
      class="mx-auto max-w-[1328px] px-5 pt-9 pb-6 sm:px-8 lg:px-12 lg:pt-12"
    >
      <div class="mb-8 flex items-end justify-between gap-4">
        <div>
          <p class="mb-2 flex items-center gap-1.5 text-xs text-zinc-500">
            Workspace<ChevronRight :size="12" />Monitoring
          </p>
          <h1 class="text-2xl font-semibold text-zinc-900">
            {{ view === 'listings' ? 'Listing activity' : 'Saved searches' }}
          </h1>
        </div>
        <div class="flex items-center gap-4">
          <span v-if="data" class="hidden text-xs text-zinc-500 md:block"
            >Last synced
            <span class="ml-1 font-mono text-zinc-700">{{
              new Date(data.updatedAt).toLocaleTimeString([], {
                hour: '2-digit',
                minute: '2-digit',
              })
            }}</span></span
          ><button class="button" :disabled="loading" @click="refresh">
            <RefreshCw :size="14" :class="{ 'animate-spin': loading }" />Refresh
          </button>
        </div>
      </div>

      <div
        v-if="error"
        role="alert"
        class="mb-6 flex items-start gap-3 rounded-md border border-red-200 bg-red-50 p-4 text-sm text-red-800"
      >
        <CircleAlert :size="18" class="mt-0.5 shrink-0" /><span
          >{{ error
          }}<template v-if="data">
            Showing the last loaded data.</template
          ></span
        ><button
          class="icon-button ml-auto"
          aria-label="Retry loading data"
          title="Retry loading data"
          :disabled="loading"
          @click="refresh"
        >
          <RefreshCw :size="16" />
        </button>
      </div>

      <section
        aria-label="Monitoring totals"
        class="mb-10 grid grid-cols-2 gap-y-7 border-b border-zinc-200 pb-8 md:grid-cols-4"
      >
        <article
          v-for="(metric, index) in metrics"
          :key="metric.name"
          class="min-w-0 pr-4"
          :class="
            index % 2 === 1
              ? 'border-l border-zinc-200 pl-5 md:pl-8'
              : index > 0
                ? 'md:border-l md:border-zinc-200 md:pl-8'
                : ''
          "
        >
          <div class="mb-3 flex items-center gap-2 text-xs text-zinc-500">
            <component
              :is="metric.icon"
              :size="15"
              class="shrink-0 text-zinc-400"
            />{{ metric.name }}
          </div>
          <p class="text-3xl leading-none font-semibold tabular-nums">
            {{ metric.value?.toLocaleString() ?? '-' }}
          </p>
          <p class="mt-2 text-xs text-zinc-500">{{ metric.detail }}</p>
        </article>
      </section>

      <section
        :aria-label="view === 'listings' ? 'Recent listings' : 'Saved searches'"
        :aria-busy="loading"
      >
        <div class="mb-5 flex items-center justify-between gap-3">
          <h2 class="flex items-center gap-2.5 text-base font-semibold">
            {{ view === 'listings' ? 'Listing feed' : 'Search subscriptions'
            }}<span
              class="rounded bg-zinc-200/60 px-1.5 py-0.5 font-mono text-[11px] font-normal text-zinc-600"
              >{{ resultCount }}</span
            >
          </h2>
          <span class="text-xs text-zinc-500">Latest 100 records</span>
        </div>

        <template v-if="loadedRecords.length">
          <div
            v-if="view === 'listings'"
            class="mb-5 flex w-fit max-w-full items-center gap-1 rounded-md bg-zinc-200/60 p-1"
            role="group"
            aria-label="Filter by category"
          >
            <button
              v-for="option in categoryOptions"
              :key="option.value"
              :aria-pressed="category === option.value"
              class="flex h-8 items-center justify-center gap-2 rounded px-3 text-xs transition-colors sm:px-4"
              :class="
                category === option.value
                  ? 'bg-white font-semibold text-zinc-900 shadow-xs'
                  : 'text-zinc-500 hover:bg-white/50 hover:text-zinc-800'
              "
              @click="category = option.value"
            >
              <component :is="option.icon" :size="14" />{{ option.label }}
            </button>
          </div>
          <div
            class="mb-4 flex gap-6 border-b border-zinc-200"
            role="group"
            aria-label="Filter by status"
          >
            <button
              v-for="option in [
                { value: 'all', label: 'All records' },
                {
                  value: 'active',
                  label: view === 'listings' ? 'Active' : 'Enabled',
                },
                {
                  value: 'inactive',
                  label: view === 'listings' ? 'Inactive' : 'Disabled',
                },
              ]"
              :key="option.value"
              :aria-pressed="status === option.value"
              class="-mb-px border-b-2 pb-3 text-xs"
              :class="
                status === option.value
                  ? 'border-accent-600 font-semibold text-zinc-900'
                  : 'border-transparent text-zinc-500 hover:text-zinc-800'
              "
              @click="status = option.value"
            >
              {{ option.label }}
            </button>
          </div>
          <div class="mb-5 flex flex-wrap items-center gap-2">
            <label
              class="flex h-9 min-w-0 basis-full items-center gap-2 rounded-md border border-zinc-200 bg-white px-3 focus-within:border-zinc-400 sm:max-w-sm sm:basis-auto sm:grow"
              ><Search :size="15" class="shrink-0 text-zinc-400" /><input
                v-model="query"
                class="w-full min-w-0 bg-transparent text-xs outline-none placeholder:text-zinc-400"
                :placeholder="
                  view === 'listings'
                    ? 'Search title, location, source...'
                    : 'Search name, user, source...'
                "
                aria-label="Search records" /><button
                v-if="query"
                class="icon-button -mr-2"
                title="Clear search"
                aria-label="Clear search"
                @click="query = ''"
              >
                <X :size="14" /></button
            ></label>
            <label
              class="flex h-9 min-w-0 items-center gap-2 rounded-md border border-zinc-200 bg-white px-3 text-zinc-500 sm:ml-auto"
              ><Globe2 :size="14" class="shrink-0" /><select
                v-model="source"
                aria-label="Filter by source"
                class="h-full max-w-36 min-w-0 appearance-none bg-transparent text-xs text-zinc-700"
              >
                <option value="all">All sources</option>
                <option v-for="item in sources" :key="item" :value="item">
                  {{ item }}
                </option></select
              ><ChevronDown :size="12" class="pointer-events-none shrink-0"
            /></label>
            <label
              v-if="view === 'listings'"
              class="flex h-9 items-center gap-2 rounded-md border border-zinc-200 bg-white px-3"
              ><select
                v-model="sort"
                aria-label="Sort listings"
                class="h-full appearance-none bg-transparent text-xs text-zinc-700"
              >
                <option value="newest">Newest first</option>
                <option value="oldest">Oldest first</option>
                <option value="title">Title A-Z</option></select
              ><ChevronDown
                :size="12"
                class="pointer-events-none text-zinc-500"
            /></label>
            <button
              v-if="hasFilters"
              class="icon-button"
              title="Reset filters"
              aria-label="Reset filters"
              @click="resetFilters"
            >
              <X :size="15" />
            </button>
          </div>
        </template>

        <div
          v-if="!data && loading"
          class="flex min-h-80 flex-col items-center justify-center gap-4 bg-white"
          role="status"
        >
          <RefreshCw :size="24" class="animate-spin text-zinc-400" />
          <h3 class="font-medium text-zinc-600">Loading workspace</h3>
        </div>
        <div
          v-else-if="!data && error"
          class="flex min-h-80 flex-col items-center justify-center gap-4 bg-white"
        >
          <Database :size="30" class="text-zinc-400" />
          <h3 class="font-medium">Workspace unavailable</h3>
          <button class="button" @click="refresh">
            <RefreshCw :size="14" />Retry connection
          </button>
        </div>
        <div
          v-else-if="resultCount === 0"
          class="relative flex min-h-80 flex-col items-center justify-center overflow-hidden border-y border-zinc-200 bg-white px-5 py-12 sm:min-h-96"
        >
          <div
            class="empty-grid pointer-events-none absolute top-3 h-56 w-80"
            aria-hidden="true"
          />
          <div aria-hidden="true" class="relative mb-6 h-24 w-28">
            <div
              class="absolute top-1 left-4 h-19 w-15 -rotate-12 rounded-md border border-zinc-200 bg-zinc-50 p-3 shadow-xs"
            >
              <div class="mt-2 h-1 rounded bg-zinc-200" />
              <div class="mt-2 h-1 rounded bg-zinc-200" />
              <div class="mt-2 h-1 w-2/3 rounded bg-zinc-200" />
            </div>
            <div
              class="absolute top-4 left-10 flex h-19 w-15 rotate-6 flex-col gap-2 rounded-md border border-zinc-300 bg-white p-3 shadow-sm"
            >
              <component
                :is="view === 'listings' ? Layers3 : SearchCheck"
                :size="22"
                class="text-zinc-400"
              />
              <div class="h-1 rounded bg-zinc-200" />
              <div class="h-1 w-2/3 rounded bg-zinc-200" />
            </div>
            <span
              class="absolute right-0 bottom-0 grid size-9 place-items-center rounded-full border-4 border-white bg-accent-100 text-accent-600"
              ><Search :size="16"
            /></span>
          </div>
          <h3 class="relative text-base font-semibold">
            {{
              hasFilters
                ? 'No matching records'
                : view === 'listings'
                  ? 'A clean slate for your listings'
                  : 'No saved searches yet'
            }}
          </h3>
          <p
            class="relative mt-2 max-w-xs text-center text-sm leading-6 text-zinc-500"
          >
            {{
              hasFilters
                ? 'No records match the current selection.'
                : view === 'listings'
                  ? 'No advertisements have been collected.'
                  : 'No search subscriptions are configured.'
            }}
          </p>
          <button
            v-if="hasFilters"
            class="button relative mt-5"
            @click="resetFilters"
          >
            <X :size="14" />Clear filters
          </button>
          <span
            v-else
            class="relative mt-6 flex items-center gap-2 text-xs text-zinc-500"
            ><Database :size="13" />{{
              data ? 'Local database connected' : 'Waiting for database'
            }}</span
          >
        </div>

        <div
          v-else-if="view === 'listings'"
          class="relative overflow-x-auto border-y border-zinc-200 bg-white"
        >
          <table class="w-full min-w-[780px] border-collapse">
            <thead class="bg-zinc-100/70">
              <tr>
                <th class="table-head w-[42%]">Listing</th>
                <th class="table-head">Source</th>
                <th class="table-head text-right">Price</th>
                <th class="table-head">Status</th>
                <th class="table-head">First seen</th>
                <th class="table-head">
                  <span class="sr-only">Original listing</span>
                </th>
              </tr>
            </thead>
            <tbody class="divide-y divide-zinc-100">
              <template v-for="listing in visibleListings" :key="listing.id">
                <tr
                  class="cursor-pointer transition-colors"
                  :class="
                    expandedListing === listing.id
                      ? 'bg-accent-50 shadow-[inset_3px_0_0_var(--color-accent-600)]'
                      : 'hover:bg-zinc-50'
                  "
                  @click="toggleListing(listing)"
                >
                  <td class="table-cell">
                    <div class="flex items-center gap-3">
                      <button
                        :id="`listing-toggle-${listing.id}`"
                        class="icon-button -ml-2"
                        :aria-label="`${expandedListing === listing.id ? 'Collapse' : 'Expand'} listing ${listing.id}`"
                        :title="
                          expandedListing === listing.id
                            ? 'Collapse details'
                            : 'Expand details'
                        "
                        :aria-expanded="expandedListing === listing.id"
                        :aria-controls="`listing-details-${listing.id}`"
                        @click.stop="toggleListing(listing)"
                      >
                        <ChevronRight
                          :size="16"
                          class="transition-transform"
                          :class="{
                            'rotate-90 text-accent-600':
                              expandedListing === listing.id,
                          }"
                        />
                      </button>
                      <div
                        class="grid size-12 shrink-0 place-items-center overflow-hidden rounded bg-zinc-100 text-zinc-400"
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
                        /><ImageOff v-else :size="19" />
                      </div>
                      <div class="min-w-0">
                        <button
                          :id="`listing-title-${listing.id}`"
                          class="line-clamp-2 text-left text-sm font-medium break-words hover:text-accent-600"
                          :aria-expanded="expandedListing === listing.id"
                          :aria-controls="`listing-details-${listing.id}`"
                          @click.stop="toggleListing(listing)"
                        >
                          {{ listing.title }}</button
                        ><span class="mt-1 block text-xs text-zinc-500"
                          >{{ listing.location || '-'
                          }}<span class="mx-1.5 text-zinc-300">/</span
                          ><span class="font-mono text-[10px]"
                            >#{{ listing.id }}</span
                          ></span
                        >
                      </div>
                    </div>
                  </td>
                  <td class="table-cell">
                    <span
                      class="inline-flex items-center gap-1.5 text-xs text-zinc-600"
                      ><Globe2 :size="13" class="shrink-0 text-zinc-400" />{{
                        listing.source
                      }}</span
                    >
                  </td>
                  <td
                    class="table-cell text-right text-sm font-medium whitespace-nowrap tabular-nums"
                  >
                    {{ listing.price || '-' }}
                  </td>
                  <td class="table-cell">
                    <span
                      class="status-badge"
                      :class="
                        listing.active
                          ? 'bg-emerald-50 text-emerald-700'
                          : 'bg-zinc-100 text-zinc-500'
                      "
                      ><span class="size-1 rounded-full bg-current" />{{
                        listing.active ? 'Active' : 'Inactive'
                      }}</span
                    >
                  </td>
                  <td
                    class="table-cell max-w-36 text-xs leading-5 text-zinc-500"
                  >
                    {{ formatTime(listing.firstSeenAt) }}
                  </td>
                  <td class="table-cell">
                    <a
                      v-if="safeURL(listing.url)"
                      class="icon-button"
                      :href="safeURL(listing.url)"
                      target="_blank"
                      rel="noopener noreferrer"
                      :aria-label="`Open ${listing.title}`"
                      title="Open original listing"
                      @click.stop
                      ><ArrowUpRight :size="17"
                    /></a>
                  </td>
                </tr>
                <tr v-if="expandedListing === listing.id" class="bg-zinc-100">
                  <td colspan="6" class="border-b-2 border-zinc-300 p-0">
                    <section
                      :id="`listing-details-${listing.id}`"
                      :aria-labelledby="`listing-title-${listing.id}`"
                      class="w-[min(100%,calc(100vw-2.5rem))] border-l-[3px] border-accent-600 md:w-full"
                    >
                      <header
                        class="flex items-center justify-between gap-3 border-b border-zinc-200 bg-zinc-100 px-4 py-2.5 sm:px-6"
                      >
                        <h3
                          class="flex flex-wrap items-center gap-2 text-xs font-semibold text-zinc-700"
                        >
                          <Layers3 :size="14" class="text-accent-600" />Listing
                          details<span
                            class="font-mono text-[11px] font-normal text-zinc-500"
                            >#{{ listing.id }}</span
                          >
                        </h3>
                        <button
                          class="icon-button"
                          aria-label="Collapse listing details"
                          title="Collapse details"
                          @click="closeListing"
                        >
                          <X :size="16" />
                        </button>
                      </header>
                      <div
                        class="grid md:grid-cols-[220px_minmax(0,1fr)] lg:grid-cols-[260px_minmax(0,1fr)]"
                      >
                        <div
                          class="min-w-0 border-b border-zinc-200 bg-zinc-50 p-4 sm:p-6 md:border-r md:border-b-0"
                        >
                          <div
                            class="mb-4 flex aspect-[4/3] w-full max-w-56 items-center justify-center overflow-hidden rounded bg-zinc-200/60"
                          >
                            <img
                              v-if="
                                safeURL(listing.imageUrl) &&
                                !failedImages.has(listing.id)
                              "
                              :src="safeURL(listing.imageUrl)"
                              :alt="listing.title"
                              class="size-full object-contain"
                              referrerpolicy="no-referrer"
                              @error="failedImages.add(listing.id)"
                            /><ImageOff
                              v-else
                              :size="26"
                              class="text-zinc-400"
                            />
                          </div>
                          <p class="text-lg font-semibold tabular-nums">
                            {{ listing.price || 'Price not recorded' }}
                          </p>
                          <span
                            class="mt-1.5 flex items-center gap-1.5 text-xs text-zinc-500"
                            ><Globe2 :size="13" />{{ listing.source }}</span
                          >
                          <a
                            v-if="safeURL(listing.url)"
                            :href="safeURL(listing.url)"
                            target="_blank"
                            rel="noopener noreferrer"
                            class="button mt-5 w-full max-w-56"
                            ><ArrowUpRight :size="14" />Original listing</a
                          >
                        </div>
                        <div class="min-w-0 bg-white">
                          <section
                            class="px-4 py-5 sm:px-6 sm:py-6"
                            :aria-labelledby="`listing-specs-${listing.id}`"
                          >
                            <h4
                              :id="`listing-specs-${listing.id}`"
                              class="mb-5 text-sm font-semibold"
                            >
                              Specifications
                            </h4>
                            <dl
                              v-if="listingAttributes.length"
                              class="grid grid-cols-2 gap-x-6 gap-y-5 lg:grid-cols-3"
                            >
                              <div
                                v-for="attribute in listingAttributes"
                                :key="attribute.key"
                                class="min-w-0"
                              >
                                <dt class="mb-1.5 text-xs text-zinc-500">
                                  {{ attribute.label }}
                                </dt>
                                <dd
                                  class="text-base font-medium whitespace-pre-wrap [overflow-wrap:anywhere]"
                                >
                                  {{ attribute.value }}
                                </dd>
                              </div>
                            </dl>
                            <p v-else class="text-sm text-zinc-500">
                              No additional attributes recorded.
                            </p>
                          </section>
                          <section
                            class="border-t border-zinc-200 px-4 py-5 sm:px-6"
                            :aria-labelledby="`listing-text-${listing.id}`"
                          >
                            <div class="mb-3 flex flex-wrap items-center gap-2">
                              <h4
                                :id="`listing-text-${listing.id}`"
                                class="text-sm font-semibold"
                              >
                                {{
                                  description ? 'Description' : 'Listing text'
                                }}
                              </h4>
                              <span
                                v-if="!description"
                                class="rounded bg-zinc-100 px-1.5 py-0.5 text-[10px] font-medium text-zinc-500"
                                >Category-page preview</span
                              >
                            </div>
                            <p
                              class="max-w-prose text-sm leading-7 break-words whitespace-pre-wrap text-zinc-600"
                            >
                              {{ description ?? listing.title }}
                            </p>
                            <p
                              v-if="!description"
                              class="mt-3 text-xs leading-5 text-zinc-500"
                            >
                              Full description not collected.
                            </p>
                          </section>
                        </div>
                      </div>
                      <footer
                        class="border-t border-zinc-200 bg-zinc-100 px-4 py-4 sm:px-6"
                      >
                        <dl
                          class="grid gap-4 text-xs sm:grid-cols-[180px_minmax(0,1fr)]"
                        >
                          <div>
                            <dt class="mb-1 text-zinc-500">First seen</dt>
                            <dd class="font-medium text-zinc-700">
                              {{ formatTime(listing.firstSeenAt) }}
                            </dd>
                          </div>
                          <div class="min-w-0">
                            <dt class="mb-1 text-zinc-500">Category path</dt>
                            <dd
                              class="font-mono text-[11px] leading-5 text-zinc-600 [overflow-wrap:anywhere]"
                            >
                              {{ listing.location || '-' }}
                            </dd>
                          </div>
                        </dl>
                        <details class="mt-4 border-t border-zinc-200 pt-3">
                          <summary
                            class="cursor-pointer text-xs font-medium text-zinc-600"
                          >
                            Raw scraped attributes
                          </summary>
                          <pre
                            class="mt-3 max-h-64 overflow-auto font-mono text-xs leading-6 whitespace-pre-wrap text-zinc-600 [overflow-wrap:anywhere]"
                            >{{
                              JSON.stringify(listing.attrs ?? {}, null, 2)
                            }}</pre>
                        </details>
                      </footer>
                    </section>
                  </td>
                </tr>
              </template>
            </tbody>
          </table>
        </div>

        <div
          v-else
          class="divide-y divide-zinc-100 border-y border-zinc-200 bg-white"
        >
          <article v-for="saved in visibleSearches" :key="saved.id">
            <button
              class="flex w-full items-center gap-3 px-3 py-5 text-left hover:bg-zinc-50 sm:gap-4 sm:px-4"
              :aria-expanded="expanded === saved.id"
              @click="expanded = expanded === saved.id ? null : saved.id"
            >
              <span
                class="grid size-9 shrink-0 place-items-center rounded bg-zinc-100 text-zinc-500"
                ><Search :size="17" /></span
              ><span class="min-w-0 flex-1"
                ><strong class="block text-sm font-medium break-words">{{
                  saved.name
                }}</strong
                ><span class="mt-1 block text-xs text-zinc-500"
                  >{{ saved.userName }} · {{ saved.source }}</span
                ></span
              ><span class="hidden text-xs text-zinc-500 sm:block"
                >{{ saved.matches }} matches</span
              ><span
                class="status-badge"
                :class="
                  saved.enabled
                    ? 'bg-emerald-50 text-emerald-700'
                    : 'bg-zinc-100 text-zinc-500'
                "
                ><Check v-if="saved.enabled" :size="12" />{{
                  saved.enabled ? 'Enabled' : 'Disabled'
                }}</span
              ><ChevronRight
                :size="15"
                class="shrink-0 text-zinc-400 transition-transform"
                :class="{ 'rotate-90': expanded === saved.id }"
              />
            </button>
            <div
              v-if="expanded === saved.id"
              class="border-t border-zinc-100 bg-zinc-50/70 px-5 py-5 sm:pl-17"
            >
              <dl class="grid gap-5 text-sm sm:grid-cols-2">
                <div>
                  <dt class="mb-1.5 text-xs font-medium text-zinc-500">
                    Channel
                  </dt>
                  <dd class="break-all">{{ saved.channel || '-' }}</dd>
                </div>
                <div>
                  <dt class="mb-1.5 text-xs font-medium text-zinc-500">
                    Filters
                  </dt>
                  <dd>
                    <pre
                      class="font-mono text-xs leading-6 break-words whitespace-pre-wrap"
                      >{{ JSON.stringify(saved.filters, null, 2) }}</pre>
                  </dd>
                </div>
                <div class="sm:col-span-2">
                  <dt class="mb-1.5 text-xs font-medium text-zinc-500">
                    Feeds
                  </dt>
                  <dd
                    v-for="feed in saved.feeds"
                    :key="feed"
                    class="break-all text-xs leading-6 text-zinc-600"
                  >
                    {{ feed }}
                  </dd>
                </div>
              </dl>
            </div>
          </article>
        </div>

        <footer
          class="flex items-center justify-between gap-4 py-4 text-xs text-zinc-500"
        >
          <span>{{
            resultCount
              ? `${(page - 1) * pageSize + 1}-${Math.min(page * pageSize, resultCount)} of ${resultCount} records`
              : '0 records'
          }}</span>
          <div v-if="resultCount > pageSize" class="flex items-center gap-3">
            <button
              class="icon-button"
              title="Previous page"
              aria-label="Previous page"
              :disabled="page === 1"
              @click="page--"
            >
              <ArrowLeft :size="15" /></button
            ><span class="font-mono text-[11px]"
              >{{ page }} / {{ pageCount }}</span
            ><button
              class="icon-button"
              title="Next page"
              aria-label="Next page"
              :disabled="page === pageCount"
              @click="page++"
            >
              <ArrowRight :size="15" />
            </button>
          </div>
          <span v-else class="flex items-center gap-1.5"
            ><RefreshCw :size="12" :class="{ 'animate-spin': loading }" />{{
              error
                ? 'Sync interrupted'
                : loading
                  ? 'Syncing'
                  : 'Auto-sync · 30s'
            }}</span
          >
        </footer>
      </section>
      <footer
        class="mt-7 flex flex-wrap items-center justify-between gap-3 border-t border-zinc-200 pt-5 text-xs text-zinc-500"
      >
        <span class="flex items-center gap-2"
          ><Radio :size="14" /><span class="font-medium text-zinc-600"
            >admonitor</span
          ><span class="text-zinc-300">/</span>Local workspace</span
        ><span class="flex items-center gap-1.5"
          ><ShieldCheck :size="13" />Read-only session</span
        >
      </footer>
    </main>
  </div>
</template>
