<script setup lang="ts">
import { computed, ref } from 'vue'
import { X } from 'lucide-vue-next'

// A searchable multi-select with chips. Typing filters the options (case and
// Latvian diacritics do not matter); Enter adds the first match, or the text
// itself when allowFree is set (e.g. "Passat*" for every Passat generation).
const props = defineProps<{
  options: string[]
  placeholder?: string
  allowFree?: boolean
  id?: string
}>()
const model = defineModel<string[]>({ default: () => [] })

const query = ref('')
const open = ref(false)
const active = ref(0)

const fold = (s: string) =>
  s
    .toLowerCase()
    .normalize('NFD')
    .replace(/\p{Diacritic}/gu, '')

const matches = computed(() => {
  const q = fold(query.value.trim())
  const chosen = new Set(model.value)
  const starts: string[] = []
  const inside: string[] = []
  for (const o of props.options) {
    if (chosen.has(o)) continue
    const f = fold(o)
    if (f.startsWith(q)) starts.push(o)
    else if (f.includes(q)) inside.push(o)
  }
  return [...starts, ...inside].slice(0, 50)
})

function add(value: string) {
  const v = value.trim()
  if (v && !model.value.includes(v)) model.value = [...model.value, v]
  query.value = ''
  active.value = 0
}

function remove(value: string) {
  model.value = model.value.filter((v) => v !== value)
}

function onInput() {
  open.value = true
  active.value = 0
}

function onEnter() {
  const pick = matches.value[active.value]
  if (pick) add(pick)
  else if (props.allowFree && query.value.trim()) add(query.value)
}

function onBackspace() {
  if (!query.value && model.value.length) model.value = model.value.slice(0, -1)
}

function move(step: number) {
  open.value = true
  const n = matches.value.length
  if (n) active.value = (active.value + step + n) % n
}
</script>

<template>
  <div class="relative">
    <div
      class="flex min-h-9 flex-wrap items-center gap-1.5 rounded-md border border-zinc-200 bg-white px-2 py-1.5 focus-within:border-accent-600"
    >
      <span
        v-for="value in model"
        :key="value"
        class="inline-flex items-center gap-1 rounded bg-zinc-100 py-0.5 pr-1 pl-2 text-xs text-zinc-800"
        >{{ value
        }}<button
          type="button"
          class="rounded p-0.5 text-zinc-500 hover:bg-zinc-200 hover:text-zinc-900"
          :aria-label="`Remove ${value}`"
          @click="remove(value)"
        >
          <X :size="12" /></button
      ></span>
      <input
        :id="id"
        v-model="query"
        class="min-w-24 flex-1 bg-transparent py-0.5 text-sm outline-none focus-visible:outline-none"
        :placeholder="model.length ? '' : placeholder"
        role="combobox"
        :aria-expanded="open && matches.length > 0"
        autocomplete="off"
        @focus="open = true"
        @blur="open = false"
        @input="onInput"
        @keydown.enter.prevent="onEnter"
        @keydown.backspace="onBackspace"
        @keydown.down.prevent="move(1)"
        @keydown.up.prevent="move(-1)"
        @keydown.esc="open = false"
      />
    </div>
    <ul
      v-if="open && (matches.length || (allowFree && query.trim()))"
      role="listbox"
      class="absolute z-20 mt-1 max-h-60 w-full overflow-auto rounded-md border border-zinc-200 bg-white py-1 shadow-lg"
    >
      <li
        v-for="(option, index) in matches"
        :key="option"
        role="option"
        :aria-selected="index === active"
        class="cursor-pointer px-3 py-1.5 text-sm"
        :class="
          index === active ? 'bg-accent-50 text-zinc-900' : 'text-zinc-700'
        "
        @mousedown.prevent="add(option)"
        @mouseenter="active = index"
      >
        {{ option }}
      </li>
      <li
        v-if="allowFree && query.trim() && !matches.includes(query.trim())"
        class="cursor-pointer border-t border-zinc-100 px-3 py-1.5 text-sm text-zinc-500"
        @mousedown.prevent="add(query)"
      >
        Use “{{ query.trim() }}”
      </li>
    </ul>
  </div>
</template>
