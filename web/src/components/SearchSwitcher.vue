<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import {
  Check,
  ChevronsUpDown,
  Plus,
  Search as SearchIcon,
} from 'lucide-vue-next'
import type { Search } from '../api'
import { navigate } from '../router'

// The current search's name in the breadcrumb; it opens a short menu for
// jumping straight to another search (or starting a new one).
const props = defineProps<{
  searches: Search[]
  current?: Search
  categoryLabel: (id: string) => string
  showOwner?: boolean
}>()

const open = ref(false)
const query = ref('')
const root = ref<HTMLElement>()
const filter = ref<HTMLInputElement>()

const visible = computed(() => {
  const q = query.value.trim().toLowerCase()
  return props.searches.filter((s) =>
    `${s.name} ${s.userName}`.toLowerCase().includes(q),
  )
})

function close() {
  open.value = false
  query.value = ''
}

function go(to: string) {
  close()
  navigate(to)
}

function onPointerDown(event: PointerEvent) {
  if (!root.value?.contains(event.target as Node)) close()
}

watch(open, async (value) => {
  if (value) {
    document.addEventListener('pointerdown', onPointerDown)
    await nextTick()
    filter.value?.focus()
  } else document.removeEventListener('pointerdown', onPointerDown)
})
onBeforeUnmount(() =>
  document.removeEventListener('pointerdown', onPointerDown),
)
</script>

<template>
  <div ref="root" class="relative min-w-0" @keydown.esc.stop="close">
    <button
      type="button"
      class="flex max-w-full min-w-0 items-center gap-1 rounded-md px-1.5 py-0.5 font-medium text-gray-900 hover:bg-gray-150/70"
      :aria-expanded="open"
      aria-haspopup="true"
      @click="open ? close() : (open = true)"
    >
      <span class="truncate">{{ current?.name ?? 'New search' }}</span>
      <ChevronsUpDown :size="14" class="shrink-0 text-gray-500" />
    </button>

    <div
      v-if="open"
      class="absolute top-full left-0 z-30 mt-1 w-80 max-w-[calc(100vw-2rem)] rounded-lg bg-white p-1 shadow-raised"
    >
      <label
        v-if="searches.length > 6"
        class="mb-1 flex h-8 items-center gap-2 border-b border-gray-150 px-2.5"
      >
        <SearchIcon :size="14" class="shrink-0 text-gray-400" />
        <input
          ref="filter"
          v-model="query"
          class="w-full bg-transparent text-sm outline-none placeholder:text-gray-400 focus-visible:outline-none"
          placeholder="Find a search"
          aria-label="Find a search"
        />
      </label>
      <ul class="max-h-80 overflow-y-auto">
        <li v-for="s in visible" :key="s.id">
          <button
            type="button"
            class="flex w-full items-center gap-3 rounded-md px-2.5 py-2 text-left hover:bg-gray-100"
            :aria-current="s.id === current?.id ? 'page' : undefined"
            @click="go(`/searches/${s.id}`)"
          >
            <span class="min-w-0 flex-1">
              <span class="block truncate text-sm text-gray-900">{{
                s.name
              }}</span>
              <span class="block truncate text-xs text-gray-500">
                {{
                  [
                    showOwner ? s.userName : '',
                    categoryLabel(s.category),
                    s.enabled ? '' : 'Paused',
                  ]
                    .filter(Boolean)
                    .join(' · ')
                }}
              </span>
            </span>
            <Check
              v-if="s.id === current?.id"
              :size="16"
              class="shrink-0 text-brand-600"
            />
          </button>
        </li>
        <li v-if="!visible.length" class="px-2.5 py-2 text-sm text-gray-500">
          No search by that name.
        </li>
      </ul>
      <div class="mt-1 border-t border-gray-150 pt-1">
        <button
          type="button"
          class="flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-sm text-gray-700 hover:bg-gray-100 hover:text-gray-900"
          @click="go('/searches/new')"
        >
          <Plus :size="16" class="text-gray-500" />New search
        </button>
      </div>
    </div>
  </div>
</template>
