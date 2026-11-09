<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  reactive,
  ref,
  watch,
} from 'vue'
import {
  ChevronRight,
  CircleAlert,
  FileQuestion,
  Trash2,
  TriangleAlert,
} from 'lucide-vue-next'
import BaseDialog from './BaseDialog.vue'
import EmptyState from './EmptyState.vue'
import MultiSelect from './MultiSelect.vue'
import PageHeader from './PageHeader.vue'
import SearchPreview from './SearchPreview.vue'
import SearchSwitcher from './SearchSwitcher.vue'
import {
  api,
  ApiError,
  describe,
  type Category,
  type Channel,
  type FieldSpec,
  type Filters,
  type Me,
  type Preview,
  type Range,
  type Search,
} from '../api'
import { navigate, onLinkClick, setLeaveGuard } from '../router'
import { showEveryone } from '../state'

// Creates or edits one search on its own page (/searches/new, /searches/12).
// The form is built from the source's catalog (/api/categories), so new
// fields on ss.com need no frontend change.
const props = defineProps<{ me: Me; id: number | 'new' }>()

const categories = ref<Category[]>([])
const channels = ref<Channel[]>([])
const searches = ref<Search[]>([])
const search = ref<Search>()
const state = ref<'loading' | 'ready' | 'missing' | 'failed'>('loading')

const name = ref('')
const category = ref('')
const channelId = ref(0)
const lists = reactive<Record<string, string[]>>({})
const ranges = reactive<Record<string, { min: string; max: string }>>({})
const error = ref('')
const saving = ref(false)
const deleting = ref(false)
const pendingLeave = ref<(() => void) | null>(null)
const nameInput = ref<HTMLInputElement>()

const fields = computed(
  () => categories.value.find((c) => c.id === category.value)?.fields ?? [],
)
const categoryLabel = (id: string) =>
  categories.value.find((c) => c.id === id)?.label ?? id
const sections = computed(() =>
  [
    {
      id: 'filters',
      title: 'Filters',
      description:
        'Leave a filter empty to match everything. Picking a make narrows the models to choose from.',
      fields: fields.value.filter((f) => !f.detail),
    },
    {
      id: 'details',
      title: 'Ad page filters',
      description:
        'Checked only for new ads that already match the filters above, because each check opens the ad page.',
      fields: fields.value.filter((f) => f.detail),
    },
  ].filter((section) => section.fields.length),
)

// Put the form's filter fields back to `filters` (empty for a new category).
function fillFilters(filters: Filters) {
  for (const key of Object.keys(lists)) delete lists[key]
  for (const key of Object.keys(ranges)) delete ranges[key]
  for (const f of fields.value) {
    const v = filters[f.key]
    if (f.kind === 'range') {
      const r = (v as Range | undefined) ?? {}
      ranges[f.key] = {
        min: r.min?.toString() ?? '',
        max: r.max?.toString() ?? '',
      }
    } else {
      lists[f.key] = Array.isArray(v) ? [...v] : []
    }
  }
}

function collect(): Filters {
  const out: Filters = {}
  for (const f of fields.value) {
    if (f.kind === 'range') {
      const r = ranges[f.key]
      const range: Range = {}
      if (r?.min.trim()) range.min = Number(r.min)
      if (r?.max.trim()) range.max = Number(r.max)
      if (range.min !== undefined || range.max !== undefined) out[f.key] = range
    } else if (lists[f.key]?.length) {
      out[f.key] = lists[f.key]
    }
  }
  return out
}

const input = () => ({
  name: name.value.trim(),
  category: category.value,
  filters: collect(),
  channelId: channelId.value,
})

// What was last loaded or saved; the form is "dirty" when it differs.
const snapshot = ref('')
const dirty = computed(
  () => state.value === 'ready' && JSON.stringify(input()) !== snapshot.value,
)
const summary = computed(() => describe(collect(), fields.value))
const currentFilters = computed(() => collect())
const preview = ref<Preview | null>(null)

async function load() {
  state.value = 'loading'
  error.value = ''
  try {
    if (!categories.value.length)
      [categories.value, channels.value] = await Promise.all([
        api.categories(),
        api.channels(),
      ])
    searches.value = await api.searches(showEveryone.value)
    let found =
      props.id === 'new'
        ? undefined
        : searches.value.find((s) => s.id === props.id)
    // An admin can open anyone's search even while listing only their own.
    if (props.id !== 'new' && !found && props.me.admin)
      found = (await api.searches(true)).find((s) => s.id === props.id)
    if (props.id !== 'new' && !found) {
      state.value = 'missing'
      return
    }
    search.value = found
    name.value = found?.name ?? ''
    category.value = found?.category ?? categories.value[0]?.id ?? ''
    channelId.value = found?.channel.id ?? channels.value[0]?.id ?? 0
    fillFilters(found?.filters ?? {})
    snapshot.value = JSON.stringify(input())
    state.value = 'ready'
    if (props.id === 'new') {
      await nextTick()
      nameInput.value?.focus()
    }
  } catch (reason) {
    error.value =
      reason instanceof ApiError ? reason.message : 'Could not load the search.'
    state.value = 'failed'
  }
}

async function save() {
  error.value = ''
  saving.value = true
  try {
    if (search.value) await api.updateSearch(search.value.id, input())
    else await api.createSearch(input())
    snapshot.value = JSON.stringify(input())
    navigate('/searches')
  } catch (reason) {
    error.value =
      reason instanceof ApiError ? reason.message : 'Could not save the search.'
  } finally {
    saving.value = false
  }
}

async function remove() {
  const target = search.value
  deleting.value = false
  if (!target) return
  try {
    await api.deleteSearch(target.id)
    snapshot.value = JSON.stringify(input())
    navigate('/searches', { replace: true })
  } catch (reason) {
    error.value =
      reason instanceof ApiError
        ? reason.message
        : 'Could not delete the search.'
  }
}

function discard() {
  const proceed = pendingLeave.value
  pendingLeave.value = null
  snapshot.value = JSON.stringify(input())
  proceed?.()
}

function onBeforeUnload(event: BeforeUnloadEvent) {
  if (dirty.value) event.preventDefault()
}

watch(
  () => props.id,
  () => void load(),
)
onMounted(() => {
  void load()
  setLeaveGuard((proceed) => {
    if (!dirty.value) return true
    pendingLeave.value = proceed
    return false
  })
  window.addEventListener('beforeunload', onBeforeUnload)
})
onBeforeUnmount(() => {
  setLeaveGuard(null)
  window.removeEventListener('beforeunload', onBeforeUnload)
})

// A make's models, a region's districts: narrowed by what is chosen there.
function optionsFor(f: FieldSpec): string[] {
  if (!f.dependsOn || !f.optionsBy) return f.options ?? []
  const parents = lists[f.dependsOn] ?? []
  const keys = parents.length ? parents : Object.keys(f.optionsBy)
  return [...new Set(keys.flatMap((k) => f.optionsBy?.[k] ?? []))].sort(
    (a, b) => a.localeCompare(b),
  )
}

function hint(f: FieldSpec): string {
  if (f.key === 'model') return 'End with * for every generation, e.g. Passat*'
  if (f.kind === 'all') return 'The car must have all of these.'
  if (f.kind === 'text') return 'Any of these words, e.g. quattro.'
  if (f.dependsOn === 'region') return 'Leave empty for the whole region.'
  return ''
}

const created = computed(() =>
  search.value
    ? new Date(search.value.createdAt).toLocaleDateString([], {
        day: 'numeric',
        month: 'long',
        year: 'numeric',
      })
    : '',
)
</script>

<template>
  <section>
    <nav
      aria-label="Breadcrumb"
      class="mb-3 flex min-w-0 items-center gap-1 text-small"
    >
      <a
        href="/searches"
        class="shrink-0 rounded-md px-1.5 py-0.5 -ml-1.5 text-gray-600 hover:bg-gray-150/70 hover:text-gray-900"
        @click="onLinkClick($event, '/searches')"
        >Searches</a
      >
      <ChevronRight :size="14" class="shrink-0 text-gray-400" />
      <SearchSwitcher
        v-if="state === 'ready' && (searches.length || search)"
        :searches="searches"
        :current="search"
        :category-label="categoryLabel"
        :show-owner="showEveryone"
      />
      <span v-else class="px-1.5 font-medium text-gray-900">{{
        id === 'new' ? 'New search' : 'Search'
      }}</span>
    </nav>

    <div v-if="state === 'loading'" role="status" aria-label="Loading">
      <div class="skeleton mb-2 h-8 w-64" />
      <div class="skeleton mb-10 h-4 w-96 max-w-full" />
      <div
        v-for="n in 2"
        :key="n"
        class="grid gap-x-12 gap-y-4 border-t border-gray-200 py-8 lg:grid-cols-[minmax(0,280px)_minmax(0,1fr)]"
      >
        <div>
          <div class="skeleton h-5 w-24" />
          <div class="skeleton mt-2 h-3 w-48" />
        </div>
        <div class="card h-48" />
      </div>
    </div>

    <div v-else-if="state === 'missing'" class="card">
      <EmptyState
        :icon="FileQuestion"
        title="This search doesn't exist"
        description="It may have been deleted, or it belongs to someone else."
      >
        <button class="btn-secondary" @click="navigate('/searches')">
          Back to searches
        </button>
      </EmptyState>
    </div>

    <div v-else-if="state === 'failed'" class="card">
      <EmptyState
        :icon="CircleAlert"
        title="Couldn't load this search"
        :description="error"
      >
        <button class="btn-secondary" @click="load">Try again</button>
      </EmptyState>
    </div>

    <form v-else @submit.prevent="save">
      <PageHeader :title="search ? search.name : 'New search'">
        <template #description>
          <template v-if="search">
            {{
              [
                showEveryone || search.userName !== me.name
                  ? `Created by ${search.userName}`
                  : '',
                `Created ${created}`,
                `${search.matches.toLocaleString()} alerts so far`,
              ]
                .filter(Boolean)
                .join(' · ')
            }}
            <span v-if="!search.enabled" class="badge-warning ml-1.5"
              >Paused</span
            >
          </template>
          <template v-else
            >Choose what to watch for. New ads that match are sent to Discord as
            soon as they're posted.</template
          >
        </template>
        <button
          v-if="search"
          type="button"
          class="btn-secondary text-danger-600 hover:text-danger-700"
          @click="deleting = true"
        >
          <Trash2 :size="15" />Delete
        </button>
      </PageHeader>

      <section
        class="grid gap-x-12 gap-y-4 border-t border-gray-200 py-8 lg:grid-cols-[minmax(0,280px)_minmax(0,1fr)]"
        aria-labelledby="general-heading"
      >
        <div>
          <h2 id="general-heading" class="text-base font-semibold">General</h2>
          <p class="mt-1 text-small text-gray-500">
            The name is only for you. Alerts go to the Discord channel or direct
            message you pick here.
          </p>
        </div>
        <div class="card grid gap-5 p-5 sm:grid-cols-2 sm:p-6">
          <label class="block sm:col-span-2">
            <span class="field-label">Name</span>
            <input
              ref="nameInput"
              v-model="name"
              class="field sm:max-w-md"
              maxlength="80"
              required
              placeholder="e.g. Family car under €7,000"
            />
          </label>
          <label class="block">
            <span class="field-label">Category</span>
            <select
              v-model="category"
              class="field"
              :disabled="!!search"
              @change="fillFilters({})"
            >
              <option v-for="c in categories" :key="c.id" :value="c.id">
                {{ c.label }}
              </option>
            </select>
            <span v-if="search" class="field-hint block"
              >Can't be changed after creating.</span
            >
          </label>
          <label class="block">
            <span class="field-label">Send alerts to</span>
            <select
              v-model="channelId"
              class="field"
              :disabled="!channels.length"
            >
              <option v-for="ch in channels" :key="ch.id" :value="ch.id">
                {{ ch.label || `${ch.kind} ${ch.target}` }}
              </option>
            </select>
          </label>
          <div
            v-if="!channels.length"
            class="message-bar-warning sm:col-span-2"
          >
            <TriangleAlert
              :size="16"
              class="mt-0.5 shrink-0 text-warning-600"
            />
            <span
              >Run <span class="kbd">/dashboard</span> in Discord once so alerts
              have somewhere to go.</span
            >
          </div>
        </div>
      </section>

      <section
        v-for="section in sections"
        :key="section.id"
        class="grid gap-x-12 gap-y-4 border-t border-gray-200 py-8 lg:grid-cols-[minmax(0,280px)_minmax(0,1fr)]"
        :aria-labelledby="`${section.id}-heading`"
      >
        <div>
          <h2 :id="`${section.id}-heading`" class="text-base font-semibold">
            {{ section.title }}
          </h2>
          <p class="mt-1 text-small text-gray-500">
            {{ section.description }}
          </p>
        </div>
        <div class="card grid gap-x-5 gap-y-5 p-5 sm:grid-cols-2 sm:p-6">
          <div v-for="f in section.fields" :key="f.key" class="min-w-0">
            <label :for="`f-${f.key}`" class="field-label"
              >{{ f.label
              }}<span
                v-if="f.required"
                class="text-danger-600"
                aria-hidden="true"
              >
                *</span
              ></label
            >
            <div v-if="f.kind === 'range'" class="flex items-center gap-2">
              <input
                :id="`f-${f.key}`"
                v-model="ranges[f.key].min"
                type="number"
                step="any"
                min="0"
                placeholder="Min"
                class="field tabular-nums"
                :aria-label="`${f.label} minimum`"
              /><span class="text-gray-400" aria-hidden="true">–</span
              ><input
                v-model="ranges[f.key].max"
                type="number"
                step="any"
                min="0"
                placeholder="Max"
                class="field tabular-nums"
                :aria-label="`${f.label} maximum`"
              />
            </div>
            <MultiSelect
              v-else
              :id="`f-${f.key}`"
              v-model="lists[f.key]"
              :options="optionsFor(f)"
              :allow-free="f.key === 'model' || f.kind === 'text'"
              :placeholder="
                f.kind === 'all'
                  ? 'Must have…'
                  : f.kind === 'text'
                    ? 'Type a word, press Enter'
                    : 'Any'
              "
            />
            <p v-if="hint(f)" class="field-hint">{{ hint(f) }}</p>
            <p
              v-if="f.kind === 'text'"
              class="mt-1.5 flex items-start gap-1.5 text-xs text-warning-700"
            >
              <TriangleAlert :size="13" class="mt-px shrink-0" />Opens every
              matching ad page, so narrow it down with make and model first.
            </p>
          </div>
        </div>
      </section>

      <section
        id="preview"
        class="grid scroll-mt-6 gap-x-12 gap-y-4 border-t border-gray-200 py-8 lg:grid-cols-[minmax(0,280px)_minmax(0,1fr)]"
        aria-labelledby="preview-heading"
      >
        <div>
          <h2 id="preview-heading" class="text-base font-semibold">Preview</h2>
          <p class="mt-1 text-small text-gray-500">
            Ads already collected that these filters would have matched, so you
            can check them before saving. Updates as you edit; nothing is sent.
          </p>
        </div>
        <SearchPreview
          :category="category"
          :category-label="categoryLabel(category)"
          :filters="currentFilters"
          :fields="fields"
          @result="preview = $event"
        />
      </section>

      <div
        class="sticky bottom-0 z-10 -mx-4 -mb-8 border-t border-gray-200 bg-white/90 backdrop-blur-md sm:-mx-8 lg:-mx-10 lg:-mb-10"
      >
        <div
          v-if="error"
          role="alert"
          class="message-bar-danger mx-4 mt-3 sm:mx-8 lg:mx-10"
        >
          <CircleAlert :size="16" class="mt-0.5 shrink-0 text-danger-600" />{{
            error
          }}
        </div>
        <div class="flex items-center gap-3 px-4 py-3 sm:px-8 lg:px-10">
          <p
            class="hidden min-w-0 flex-1 truncate text-small text-gray-600 md:block"
            :title="summary"
          >
            <a
              v-if="preview && preview.inScope"
              href="#preview"
              class="mr-1 font-medium text-brand-700 tabular-nums hover:underline"
              >{{ preview.matched.toLocaleString() }} collected
              {{ preview.matched === 1 ? 'ad matches' : 'ads match' }}</a
            ><span v-if="preview && preview.inScope" class="mr-1 text-gray-400"
              >·</span
            >
            <span v-if="!preview?.inScope" class="text-gray-500">Matches</span>
            {{
              summary === 'Everything'
                ? preview?.inScope
                  ? 'Every ad'
                  : 'every new ad'
                : summary
            }}
          </p>
          <span
            v-if="dirty"
            class="ml-auto flex items-center gap-1.5 text-small text-gray-500 md:ml-0"
            ><span class="size-1.5 rounded-full bg-warning-600" />Unsaved
            changes</span
          >
          <button
            type="button"
            class="btn-secondary"
            :class="{ 'ml-auto md:ml-0': !dirty }"
            @click="navigate('/searches')"
          >
            Cancel
          </button>
          <button
            type="submit"
            class="btn-primary min-w-28"
            :disabled="saving || !channels.length || (!!search && !dirty)"
          >
            {{ saving ? 'Saving…' : search ? 'Save changes' : 'Create search' }}
          </button>
        </div>
      </div>
    </form>

    <BaseDialog
      v-if="pendingLeave"
      variant="modal"
      title="Discard changes?"
      @close="pendingLeave = null"
    >
      <p class="text-sm text-gray-600">
        Your changes to this search haven't been saved.
      </p>
      <template #footer>
        <button class="btn-secondary" @click="pendingLeave = null">
          Keep editing
        </button>
        <button class="btn-danger" @click="discard">Discard</button>
      </template>
    </BaseDialog>

    <BaseDialog
      v-if="deleting && search"
      variant="modal"
      title="Delete search?"
      light-dismiss
      @close="deleting = false"
    >
      <p class="text-sm text-gray-600">
        “{{ search.name }}” and its alert history will be deleted. This can't be
        undone.
      </p>
      <template #footer>
        <button class="btn-secondary" @click="deleting = false">Cancel</button>
        <button class="btn-danger" @click="remove">Delete</button>
      </template>
    </BaseDialog>
  </section>
</template>
