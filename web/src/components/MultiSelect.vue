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

const input = ref<HTMLInputElement>()
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

const listShown = computed(
  () =>
    open.value &&
    (matches.value.length > 0 || (!!props.allowFree && !!query.value.trim())),
)

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

// Esc closes the list first; a second Esc reaches the surrounding dialog.
function onEscape(event: KeyboardEvent) {
  if (!listShown.value) return
  event.preventDefault()
  event.stopPropagation()
  open.value = false
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
      class="field-within flex min-h-8 flex-wrap items-center gap-1 px-1 py-[3px]"
      @mousedown.self.prevent="input?.focus()"
    >
      <span
        v-for="value in model"
        :key="value"
        class="inline-flex h-6 items-center gap-0.5 rounded bg-gray-100 pr-0.5 pl-2 text-small text-gray-900"
        >{{ value
        }}<button
          type="button"
          class="grid size-5 place-items-center rounded-sm text-gray-500 hover:bg-gray-200 hover:text-gray-900"
          :aria-label="`Remove ${value}`"
          @click="remove(value)"
        >
          <X :size="12" /></button
      ></span>
      <input
        :id="id"
        ref="input"
        v-model="query"
        class="h-6 min-w-24 flex-1 bg-transparent px-1.5 text-sm outline-none placeholder:text-gray-400 focus-visible:outline-none"
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
        @keydown.esc="onEscape"
      />
    </div>
    <ul
      v-if="listShown"
      role="listbox"
      class="absolute z-20 mt-1 max-h-64 w-full overflow-auto rounded-lg bg-white p-1 shadow-raised"
    >
      <li
        v-for="(option, index) in matches"
        :key="option"
        role="option"
        :aria-selected="index === active"
        class="cursor-pointer rounded-md px-2.5 py-1.5 text-sm text-gray-900"
        :class="{ 'bg-gray-100': index === active }"
        @mousedown.prevent="add(option)"
        @mouseenter="active = index"
      >
        {{ option }}
      </li>
      <li
        v-if="allowFree && query.trim() && !matches.includes(query.trim())"
        class="cursor-pointer rounded-md px-2.5 py-1.5 text-sm text-gray-600 hover:bg-gray-100"
        :class="{ 'mt-1 border-t border-gray-100': matches.length }"
        @mousedown.prevent="add(query)"
      >
        Use “<span class="text-gray-900">{{ query.trim() }}</span
        >”
      </li>
    </ul>
  </div>
</template>
