<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { X } from 'lucide-vue-next'

// A native modal <dialog>: a side sheet for editors and details, or a small
// centered box for confirmations. Mount it with v-if; Esc and the close
// button emit `close`, and focus goes back where it was when it unmounts.
const props = withDefaults(
  defineProps<{
    title: string
    subtitle?: string
    variant?: 'sheet' | 'modal'
    // Clicking the backdrop closes it. Off for forms, so work isn't lost.
    lightDismiss?: boolean
  }>(),
  { variant: 'sheet', subtitle: '', lightDismiss: false },
)
const emit = defineEmits<{ close: [] }>()

const dialog = ref<HTMLDialogElement>()
const returnFocus = document.activeElement as HTMLElement | null

onMounted(() => dialog.value?.showModal())
onBeforeUnmount(() => {
  dialog.value?.close()
  returnFocus?.focus({ preventScroll: true })
})

function onCancel(event: Event) {
  event.preventDefault()
  emit('close')
}

function onClick(event: MouseEvent) {
  if (props.lightDismiss && event.target === dialog.value) emit('close')
}
</script>

<template>
  <dialog
    ref="dialog"
    :class="variant"
    :aria-label="title"
    @cancel="onCancel"
    @click="onClick"
  >
    <div class="flex h-full max-h-[inherit] flex-col">
      <header
        class="flex shrink-0 items-start gap-4"
        :class="variant === 'sheet' ? 'px-6 pt-5 pb-4' : 'px-5 pt-5 pb-2'"
      >
        <div class="min-w-0 flex-1">
          <h2 class="text-title font-semibold text-gray-900">{{ title }}</h2>
          <p
            v-if="subtitle"
            class="mt-0.5 truncate text-sm text-gray-500"
            :title="subtitle"
          >
            {{ subtitle }}
          </p>
        </div>
        <button
          v-if="variant === 'sheet'"
          type="button"
          class="btn-icon -mr-2"
          aria-label="Close"
          title="Close"
          @click="emit('close')"
        >
          <X :size="18" />
        </button>
      </header>
      <div
        class="min-h-0 flex-1"
        :class="
          variant === 'sheet' ? 'overflow-y-auto px-6 pt-1 pb-8' : 'px-5 pb-5'
        "
      >
        <slot />
      </div>
      <footer
        v-if="$slots.footer"
        class="flex shrink-0 items-center justify-end gap-2"
        :class="
          variant === 'sheet'
            ? 'border-t border-gray-150 bg-gray-25 px-6 py-3.5'
            : 'bg-gray-25 px-5 py-3.5 shadow-[inset_0_1px_0_var(--color-gray-150)]'
        "
      >
        <slot name="footer" />
      </footer>
    </div>
  </dialog>
</template>
