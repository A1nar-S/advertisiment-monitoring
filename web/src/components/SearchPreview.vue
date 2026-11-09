<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import {
  ArrowUpRight,
  CircleAlert,
  ImageOff,
  Info,
  SearchX,
} from 'lucide-vue-next'
import {
  ApiError,
  previewSearch,
  type FieldSpec,
  type Filters,
  type Preview,
} from '../api'

// Which already collected ads the search, as it is being edited, would have
// matched. Re-checked a moment after each change; nothing is saved or sent.
const props = defineProps<{
  category: string
  categoryLabel: string
  filters: Filters
  fields: FieldSpec[]
}>()
const emit = defineEmits<{ result: [Preview | null] }>()

const preview = ref<Preview | null>(null)
const loading = ref(false)
const error = ref('')
const broken = ref(new Set<number>())
let controller: AbortController | undefined
let timer: ReturnType<typeof setTimeout> | undefined

async function run() {
  controller?.abort()
  controller = new AbortController()
  loading.value = true
  error.value = ''
  try {
    preview.value = await previewSearch(
      { category: props.category, filters: props.filters },
      controller.signal,
    )
  } catch (reason) {
    if (reason instanceof DOMException && reason.name === 'AbortError') return
    preview.value = null
    error.value =
      reason instanceof ApiError
        ? reason.message
        : 'Could not check the collected ads.'
  }
  loading.value = false
  emit('result', preview.value)
}

watch(
  () => JSON.stringify([props.category, props.filters]),
  () => {
    clearTimeout(timer)
    timer = setTimeout(run, preview.value ? 400 : 0)
  },
  { immediate: true },
)
onBeforeUnmount(() => {
  clearTimeout(timer)
  controller?.abort()
})

const label = (key: string) =>
  props.fields.find((f) => f.key === key)?.label ?? key
const unsureLabels = computed(() =>
  (preview.value?.unsureKeys ?? []).map(label).join(', '),
)
const since = computed(() =>
  preview.value?.since
    ? new Date(preview.value.since).toLocaleDateString([], {
        day: 'numeric',
        month: 'long',
      })
    : '',
)
// "Cars" → "car", for "938 collected car ads".
const kind = computed(() => props.categoryLabel.toLowerCase().replace(/s$/, ''))

function seen(at: string) {
  return new Date(at).toLocaleString([], {
    day: 'numeric',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
  })
}
</script>

<template>
  <div class="card overflow-hidden" :aria-busy="loading">
    <div
      class="flex items-start gap-4 border-b border-gray-150 px-5 py-4 sm:px-6"
    >
      <div class="min-w-0 flex-1">
        <p
          v-if="preview && preview.inScope"
          class="text-sm text-gray-900"
          aria-live="polite"
        >
          <span class="font-semibold tabular-nums">{{
            preview.matched.toLocaleString()
          }}</span>
          of {{ preview.inScope.toLocaleString() }} collected {{ kind }} ads
          match<template v-if="preview.unsure">
            <span class="text-gray-500">
              · {{ preview.unsure.toLocaleString() }} more unchecked</span
            ></template
          >
        </p>
        <p v-else-if="preview" class="text-sm text-gray-900">
          No collected {{ kind }} ads yet
        </p>
        <div v-else-if="!error" class="skeleton h-4 w-56" />
        <p v-if="since" class="mt-0.5 text-xs text-gray-500">
          From the {{ preview?.scanned.toLocaleString() }} newest ads collected
          since {{ since }}
        </p>
      </div>
      <span
        v-if="loading"
        class="mt-0.5 size-4 shrink-0 animate-spin rounded-full border-2 border-gray-200 border-t-brand-600"
        role="status"
        aria-label="Checking"
      />
    </div>

    <div v-if="error" role="alert" class="message-bar-danger m-4">
      <CircleAlert :size="16" class="mt-0.5 shrink-0 text-danger-600" />{{
        error
      }}
    </div>

    <template v-else-if="preview">
      <div
        v-if="preview.unsure"
        class="flex items-start gap-2.5 border-b border-gray-150 bg-gray-25 px-5 py-3 text-small text-gray-700 sm:px-6"
      >
        <Info :size="16" class="mt-0.5 shrink-0 text-gray-500" />
        <span
          >{{ preview.unsure.toLocaleString() }} more match everything the list
          page shows, but their ad page was never read to check
          {{ unsureLabels }}. They're marked “Unchecked”. New ads are always
          checked before you're alerted.</span
        >
      </div>

      <div
        v-if="!preview.inScope"
        class="flex flex-col items-center px-6 py-10 text-center"
      >
        <SearchX :size="20" class="mb-3 text-gray-400" />
        <p class="text-sm font-medium text-gray-900">Nothing to check yet</p>
        <p class="mt-1 max-w-sm text-small text-gray-500">
          ss.com is only read for what someone's search watches, so there's no
          history here yet. Save the search and new ads start arriving.
        </p>
      </div>
      <div
        v-else-if="!preview.ads.length"
        class="flex flex-col items-center px-6 py-10 text-center"
      >
        <SearchX :size="20" class="mb-3 text-gray-400" />
        <p class="text-sm font-medium text-gray-900">No matches</p>
        <p class="mt-1 max-w-sm text-small text-gray-500">
          None of the collected ads fit these filters. Try widening one, such as
          the price or year range.
        </p>
      </div>

      <ul v-else class="max-h-[480px] divide-y divide-gray-100 overflow-y-auto">
        <li v-for="ad in preview.ads" :key="ad.id">
          <a
            :href="ad.url"
            target="_blank"
            rel="noopener noreferrer"
            class="group flex items-center gap-3.5 px-5 py-2.5 transition-colors hover:bg-gray-25 sm:px-6"
          >
            <div
              class="grid h-10 w-14 shrink-0 place-items-center overflow-hidden rounded-md bg-gray-100 text-gray-400"
            >
              <img
                v-if="ad.imageUrl && !broken.has(ad.id)"
                :src="ad.imageUrl"
                alt=""
                class="size-full object-cover"
                loading="lazy"
                referrerpolicy="no-referrer"
                @error="broken.add(ad.id)"
              />
              <ImageOff v-else :size="14" />
            </div>
            <div class="min-w-0 flex-1">
              <p class="truncate text-sm text-gray-900">{{ ad.title }}</p>
              <p class="mt-0.5 truncate text-xs text-gray-500 tabular-nums">
                {{
                  [seen(ad.firstSeen), ad.active ? '' : 'No longer listed']
                    .filter(Boolean)
                    .join(' · ')
                }}
              </p>
            </div>
            <span v-if="!ad.sure" class="badge-warning hidden sm:inline-flex"
              >Unchecked</span
            >
            <span
              v-if="ad.price"
              class="shrink-0 text-sm font-semibold text-gray-900 tabular-nums"
              >{{ ad.price }}</span
            >
            <ArrowUpRight
              :size="15"
              class="shrink-0 text-gray-400 transition-colors group-hover:text-brand-600"
              aria-hidden="true"
            />
          </a>
        </li>
      </ul>
      <p
        v-if="preview.ads.length < preview.matched + preview.unsure"
        class="border-t border-gray-150 px-5 py-2.5 text-xs text-gray-500 sm:px-6"
      >
        Showing the newest {{ preview.ads.length }} of
        {{ (preview.matched + preview.unsure).toLocaleString() }}.
      </p>
    </template>
  </div>
</template>
