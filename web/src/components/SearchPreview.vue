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
import { fieldLabel, formatDate, t, tKey } from '../i18n'

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
      reason instanceof ApiError ? reason.message : t('preview.failed')
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
  fieldLabel(key, props.fields.find((f) => f.key === key)?.label ?? key)
const unsureLabels = computed(() =>
  (preview.value?.unsureKeys ?? []).map(label).join(', '),
)
const since = computed(() =>
  preview.value?.since
    ? formatDate(preview.value.since, {
        day: 'numeric',
        month: 'long',
      })
    : '',
)
// The category as an adjective: "938 collected car ads".
const kind = computed(() =>
  tKey(
    `preview.kinds.${props.category}`,
    props.categoryLabel.toLowerCase().replace(/s$/, ''),
  ),
)

function seen(at: string) {
  return formatDate(at, {
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
          {{
            t('preview.matchLine', {
              n: preview.matched,
              matched: preview.matched,
              inScope: preview.inScope,
              kind,
            })
          }}<span v-if="preview.unsure" class="text-gray-500">
            · {{ t('preview.moreUnchecked', { n: preview.unsure }) }}</span
          >
        </p>
        <p v-else-if="preview" class="text-sm text-gray-900">
          {{ t('preview.noneYet', { kind }) }}
        </p>
        <div v-else-if="!error" class="skeleton h-4 w-56" />
        <p v-if="since && preview" class="mt-0.5 text-xs text-gray-500">
          {{ t('preview.from', { n: preview.scanned, date: since }) }}
        </p>
      </div>
      <span
        v-if="loading"
        class="mt-0.5 size-4 shrink-0 animate-spin rounded-full border-2 border-gray-200 border-t-brand-600"
        role="status"
        :aria-label="t('preview.checking')"
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
        <span>{{
          t('preview.uncheckedNote', { n: preview.unsure, keys: unsureLabels })
        }}</span>
      </div>

      <div
        v-if="!preview.inScope"
        class="flex flex-col items-center px-6 py-10 text-center"
      >
        <SearchX :size="20" class="mb-3 text-gray-400" />
        <p class="text-sm font-medium text-gray-900">
          {{ t('preview.nothingTitle') }}
        </p>
        <p class="mt-1 max-w-sm text-small text-gray-500">
          {{ t('preview.nothingBody') }}
        </p>
      </div>
      <div
        v-else-if="!preview.ads.length"
        class="flex flex-col items-center px-6 py-10 text-center"
      >
        <SearchX :size="20" class="mb-3 text-gray-400" />
        <p class="text-sm font-medium text-gray-900">
          {{ t('preview.noMatchTitle') }}
        </p>
        <p class="mt-1 max-w-sm text-small text-gray-500">
          {{ t('preview.noMatchBody') }}
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
                  [
                    seen(ad.firstSeen),
                    ad.active ? '' : t('preview.noLongerListed'),
                  ]
                    .filter(Boolean)
                    .join(' · ')
                }}
              </p>
            </div>
            <span v-if="!ad.sure" class="badge-warning hidden sm:inline-flex">{{
              t('preview.unchecked')
            }}</span>
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
        {{
          t('preview.showing', {
            shown: preview.ads.length,
            total: preview.matched + preview.unsure,
          })
        }}
      </p>
    </template>
  </div>
</template>
