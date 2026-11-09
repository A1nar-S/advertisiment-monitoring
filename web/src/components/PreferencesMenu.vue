<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { Monitor, Moon, Settings2, Sun } from 'lucide-vue-next'
import { locale, locales, setLocale, t } from '../i18n'
import { setThemeMode, themeMode, type ThemeMode } from '../theme'

// A small popover with the theme and language choices, for the sidebar, the
// mobile top bar and the sign-in page.
withDefaults(defineProps<{ placement?: 'up' | 'down' }>(), {
  placement: 'down',
})

const open = ref(false)
const root = ref<HTMLElement>()
const themes: { id: ThemeMode; icon: typeof Sun }[] = [
  { id: 'light', icon: Sun },
  { id: 'dark', icon: Moon },
  { id: 'auto', icon: Monitor },
]

function onPointerDown(event: PointerEvent) {
  if (!root.value?.contains(event.target as Node)) open.value = false
}
watch(open, (value) =>
  value
    ? document.addEventListener('pointerdown', onPointerDown)
    : document.removeEventListener('pointerdown', onPointerDown),
)
onBeforeUnmount(() =>
  document.removeEventListener('pointerdown', onPointerDown),
)
</script>

<template>
  <div ref="root" class="relative" @keydown.esc.stop="open = false">
    <button
      type="button"
      class="btn-icon"
      :aria-label="t('prefs.title')"
      :title="t('prefs.title')"
      :aria-expanded="open"
      aria-haspopup="true"
      @click="open = !open"
    >
      <Settings2 :size="16" />
    </button>

    <div
      v-if="open"
      class="absolute z-40 w-64 rounded-lg bg-surface p-3 shadow-raised"
      :class="
        placement === 'up' ? 'bottom-full left-0 mb-2' : 'top-full right-0 mt-2'
      "
      role="dialog"
      :aria-label="t('prefs.title')"
    >
      <p class="mb-1.5 text-xs font-medium text-gray-500">
        {{ t('prefs.theme') }}
      </p>
      <div
        class="grid grid-cols-3 rounded-md bg-gray-150/80 p-0.5"
        role="radiogroup"
        :aria-label="t('prefs.theme')"
      >
        <button
          v-for="option in themes"
          :key="option.id"
          type="button"
          role="radio"
          :aria-checked="themeMode === option.id"
          class="flex h-7 items-center justify-center gap-1.5 rounded-[5px] text-small transition-colors"
          :class="
            themeMode === option.id
              ? 'bg-raised font-medium text-gray-900 shadow-card'
              : 'text-gray-600 hover:text-gray-900'
          "
          @click="setThemeMode(option.id)"
        >
          <component :is="option.icon" :size="14" />{{
            t(`prefs.${option.id}`)
          }}
        </button>
      </div>
      <p v-if="themeMode === 'auto'" class="mt-1.5 text-xs text-gray-500">
        {{ t('prefs.autoHint') }}
      </p>

      <p class="mt-4 mb-1.5 text-xs font-medium text-gray-500">
        {{ t('prefs.language') }}
      </p>
      <div
        class="grid grid-cols-2 rounded-md bg-gray-150/80 p-0.5"
        role="radiogroup"
        :aria-label="t('prefs.language')"
      >
        <button
          v-for="option in locales"
          :key="option.id"
          type="button"
          role="radio"
          :lang="option.id"
          :aria-checked="locale === option.id"
          class="h-7 rounded-[5px] text-small transition-colors"
          :class="
            locale === option.id
              ? 'bg-raised font-medium text-gray-900 shadow-card'
              : 'text-gray-600 hover:text-gray-900'
          "
          @click="setLocale(option.id)"
        >
          {{ option.label }}
        </button>
      </div>
    </div>
  </div>
</template>
