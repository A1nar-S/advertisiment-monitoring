<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { CircleAlert, Info, Save, TriangleAlert, X } from 'lucide-vue-next'
import MultiSelect from './MultiSelect.vue'
import {
  api,
  ApiError,
  type Category,
  type Channel,
  type FieldSpec,
  type Filters,
  type Range,
  type Search,
} from '../api'

// Creates or edits one search. The form is built from the source's catalog
// (/api/categories), so new fields on ss.com need no frontend change.
const props = defineProps<{
  categories: Category[]
  channels: Channel[]
  search?: Search
}>()
const emit = defineEmits<{ saved: [Search]; cancel: [] }>()

const name = ref(props.search?.name ?? '')
const category = ref(props.search?.category ?? props.categories[0]?.id ?? '')
const channelId = ref(props.search?.channel.id ?? props.channels[0]?.id ?? 0)
const lists = reactive<Record<string, string[]>>({})
const ranges = reactive<Record<string, { min: string; max: string }>>({})
const error = ref('')
const saving = ref(false)

const fields = computed(
  () => props.categories.find((c) => c.id === category.value)?.fields ?? [],
)
const cheapFields = computed(() => fields.value.filter((f) => !f.detail))
const detailFields = computed(() => fields.value.filter((f) => f.detail))

// Reset the form to a search (or empty) whenever the category changes.
function load(filters: Filters) {
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
load(props.search?.filters ?? {})
watch(category, () => load({}))

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
  if (f.kind === 'all') return 'The car must have all of these'
  if (f.kind === 'text') return 'Any of these words, e.g. quattro'
  if (f.dependsOn === 'region') return 'Leave empty for the whole region'
  return ''
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

async function save() {
  error.value = ''
  saving.value = true
  const input = {
    name: name.value,
    category: category.value,
    filters: collect(),
    channelId: channelId.value,
  }
  try {
    const saved = props.search
      ? await api.updateSearch(props.search.id, input)
      : await api.createSearch(input)
    emit('saved', saved)
  } catch (reason) {
    error.value =
      reason instanceof ApiError ? reason.message : 'Could not save the search.'
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <form
    class="rounded-lg border border-zinc-200 bg-white p-5 shadow-xs sm:p-6"
    @submit.prevent="save"
  >
    <div class="mb-5 flex items-start justify-between gap-4">
      <h2 class="text-lg font-semibold">
        {{ search ? `Edit “${search.name}”` : 'New search' }}
      </h2>
      <button
        type="button"
        class="icon-button"
        aria-label="Close"
        @click="emit('cancel')"
      >
        <X :size="16" />
      </button>
    </div>

    <div class="grid gap-4 sm:grid-cols-3">
      <label class="block sm:col-span-1">
        <span class="mb-1.5 block text-xs font-medium text-zinc-600"
          >Category</span
        >
        <select
          v-model="category"
          class="h-9 w-full rounded-md border border-zinc-200 bg-white px-2"
          :disabled="!!search"
        >
          <option v-for="c in categories" :key="c.id" :value="c.id">
            {{ c.label }}
          </option>
        </select>
      </label>
      <label class="block sm:col-span-1">
        <span class="mb-1.5 block text-xs font-medium text-zinc-600"
          >Search name</span
        >
        <input
          v-model="name"
          maxlength="80"
          required
          placeholder="e.g. Cheap Skoda for parents"
          class="h-9 w-full rounded-md border border-zinc-200 px-3"
        />
        <span class="mt-1 block text-xs text-zinc-500"
          >Your own label for this search, so you can tell it apart from your
          others. It doesn't filter anything.</span
        >
      </label>
      <label class="block sm:col-span-1">
        <span class="mb-1.5 block text-xs font-medium text-zinc-600"
          >Alerts go to</span
        >
        <select
          v-model="channelId"
          class="h-9 w-full rounded-md border border-zinc-200 bg-white px-2"
        >
          <option v-for="ch in channels" :key="ch.id" :value="ch.id">
            {{ ch.label || `${ch.kind} ${ch.target}` }}
          </option>
        </select>
      </label>
    </div>

    <template
      v-for="group in [cheapFields, detailFields]"
      :key="group === cheapFields ? 'cheap' : 'detail'"
    >
      <div
        v-if="group === detailFields && detailFields.length"
        class="mt-6 mb-1 flex items-center gap-2 border-t border-zinc-100 pt-5 text-xs text-zinc-500"
      >
        <Info :size="14" />From the ad page: checked only for new ads that
        already match everything above.
      </div>
      <div class="mt-4 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        <div v-for="f in group" :key="f.key" class="block">
          <label
            :for="`f-${f.key}`"
            class="mb-1.5 block text-xs font-medium text-zinc-600"
            >{{ f.label
            }}<span v-if="f.required" class="text-accent-600"> *</span></label
          >
          <div v-if="f.kind === 'range'" class="flex items-center gap-2">
            <input
              :id="`f-${f.key}`"
              v-model="ranges[f.key].min"
              type="number"
              step="any"
              min="0"
              placeholder="min"
              class="h-9 w-full rounded-md border border-zinc-200 px-3"
            /><span class="text-zinc-400">–</span
            ><input
              v-model="ranges[f.key].max"
              type="number"
              step="any"
              min="0"
              placeholder="max"
              class="h-9 w-full rounded-md border border-zinc-200 px-3"
              :aria-label="`${f.label} max`"
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
          <p v-if="hint(f)" class="mt-1 text-xs text-zinc-400">{{ hint(f) }}</p>
          <p
            v-if="f.kind === 'text'"
            class="mt-1 flex items-center gap-1 text-xs font-medium text-amber-700"
          >
            <TriangleAlert :size="12" class="shrink-0" />Loads ad pages! Add make
            and model.
          </p>
        </div>
      </div>
    </template>

    <div
      v-if="error"
      role="alert"
      class="mt-5 flex items-start gap-2 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-800"
    >
      <CircleAlert :size="16" class="mt-0.5 shrink-0" />{{ error }}
    </div>

    <div class="mt-6 flex items-center justify-end gap-3">
      <button type="button" class="button" @click="emit('cancel')">
        Cancel
      </button>
      <button
        type="submit"
        class="button border-accent-600 bg-accent-600 text-white hover:bg-accent-700"
        :disabled="saving || !channels.length"
      >
        <Save :size="14" />{{ search ? 'Save changes' : 'Create search' }}
      </button>
    </div>
    <p v-if="!channels.length" class="mt-3 text-right text-xs text-zinc-500">
      Run <code>/dashboard</code> in Discord once so I know where to send your
      alerts.
    </p>
  </form>
</template>
