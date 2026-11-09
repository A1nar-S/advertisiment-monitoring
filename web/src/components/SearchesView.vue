<script setup lang="ts">
import { onMounted, ref } from 'vue'
import {
  CircleAlert,
  Pause,
  Pencil,
  Play,
  Plus,
  SearchCheck,
  Trash2,
} from 'lucide-vue-next'
import SearchEditor from './SearchEditor.vue'
import {
  api,
  ApiError,
  describe,
  type Category,
  type Channel,
  type Me,
  type Search,
} from '../api'

const props = defineProps<{ me: Me }>()

const searches = ref<Search[]>([])
const categories = ref<Category[]>([])
const channels = ref<Channel[]>([])
const everyone = ref(false)
const editing = ref<Search | 'new' | null>(null)
const error = ref('')
const loading = ref(true)

const fieldsOf = (category: string) =>
  categories.value.find((c) => c.id === category)?.fields
const categoryLabel = (id: string) =>
  categories.value.find((c) => c.id === id)?.label ?? id

async function load() {
  error.value = ''
  try {
    ;[searches.value, categories.value, channels.value] = await Promise.all([
      api.searches(everyone.value),
      api.categories(),
      api.channels(),
    ])
  } catch (reason) {
    error.value =
      reason instanceof ApiError
        ? reason.message
        : 'Could not load your searches.'
  } finally {
    loading.value = false
  }
}

async function act(fn: () => Promise<void>) {
  error.value = ''
  try {
    await fn()
    await load()
  } catch (reason) {
    error.value =
      reason instanceof ApiError ? reason.message : 'That did not work.'
  }
}

function toggle(s: Search) {
  void act(() => api.setEnabled(s.id, !s.enabled))
}

function remove(s: Search) {
  if (confirm(`Delete “${s.name}”? Its alert history goes too.`))
    void act(() => api.deleteSearch(s.id))
}

function saved() {
  editing.value = null
  void load()
}

onMounted(load)
</script>

<template>
  <section>
    <div class="mb-6 flex flex-wrap items-end justify-between gap-4">
      <div>
        <h1 class="text-2xl font-semibold text-zinc-900">Your searches</h1>
        <p class="mt-1 text-sm text-zinc-500">
          New ss.com ads that match arrive in Discord as they are posted.
          <template v-if="me.maxSearches && !me.admin">
            {{ searches.length }} of {{ me.maxSearches }} used.</template
          >
        </p>
      </div>
      <div class="flex items-center gap-3">
        <label
          v-if="me.admin"
          class="flex items-center gap-2 text-xs text-zinc-600"
          ><input
            v-model="everyone"
            type="checkbox"
            @change="load"
          />Everyone's</label
        >
        <button
          class="button border-accent-600 bg-accent-600 text-white hover:bg-accent-700"
          :disabled="editing !== null"
          @click="editing = 'new'"
        >
          <Plus :size="14" />New search
        </button>
      </div>
    </div>

    <div
      v-if="error"
      role="alert"
      class="mb-5 flex items-start gap-2 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-800"
    >
      <CircleAlert :size="16" class="mt-0.5 shrink-0" />{{ error }}
    </div>

    <SearchEditor
      v-if="editing !== null && categories.length"
      class="mb-6"
      :categories="categories"
      :channels="channels"
      :search="editing === 'new' ? undefined : editing"
      @saved="saved"
      @cancel="editing = null"
    />

    <p v-if="loading" class="text-sm text-zinc-500">Loading…</p>
    <div
      v-else-if="!searches.length && editing === null"
      class="rounded-lg border border-dashed border-zinc-300 bg-white p-10 text-center"
    >
      <SearchCheck :size="28" class="mx-auto mb-3 text-zinc-400" />
      <p class="font-medium">No searches yet</p>
      <p class="mt-1 text-sm text-zinc-500">
        Create one here, or type <code>/watch cars</code> in Discord.
      </p>
    </div>

    <ul class="grid gap-3">
      <li
        v-for="s in searches"
        :key="s.id"
        class="flex flex-wrap items-center gap-4 rounded-lg border border-zinc-200 bg-white p-4"
        :class="{ 'opacity-60': !s.enabled }"
      >
        <div class="min-w-0 flex-1">
          <div class="flex flex-wrap items-center gap-2">
            <span class="font-semibold">{{ s.name }}</span>
            <span class="status-badge bg-zinc-100 text-zinc-600">{{
              categoryLabel(s.category)
            }}</span>
            <span
              v-if="!s.enabled"
              class="status-badge bg-amber-50 text-amber-700"
              >Paused</span
            >
            <span v-if="everyone" class="text-xs text-zinc-500"
              >· {{ s.userName }}</span
            >
          </div>
          <p
            class="mt-1 truncate text-sm text-zinc-600"
            :title="describe(s.filters, fieldsOf(s.category))"
          >
            {{ describe(s.filters, fieldsOf(s.category)) }}
          </p>
          <p class="mt-1 text-xs text-zinc-400">
            → {{ s.channel.label || s.channel.target }} · {{ s.matches }} alerts
          </p>
        </div>
        <div class="flex items-center gap-1">
          <button
            class="icon-button"
            :aria-label="s.enabled ? `Pause ${s.name}` : `Resume ${s.name}`"
            :title="s.enabled ? 'Pause' : 'Resume'"
            @click="toggle(s)"
          >
            <Pause v-if="s.enabled" :size="16" /><Play v-else :size="16" />
          </button>
          <button
            class="icon-button"
            :aria-label="`Edit ${s.name}`"
            title="Edit"
            :disabled="editing !== null"
            @click="editing = s"
          >
            <Pencil :size="16" />
          </button>
          <button
            class="icon-button hover:text-red-700"
            :aria-label="`Delete ${s.name}`"
            title="Delete"
            @click="remove(s)"
          >
            <Trash2 :size="16" />
          </button>
        </div>
      </li>
    </ul>
  </section>
</template>
