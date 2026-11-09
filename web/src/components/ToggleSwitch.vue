<script setup lang="ts">
defineProps<{ label: string; disabled?: boolean }>()
const checked = defineModel<boolean>({ required: true })
</script>

<template>
  <button
    type="button"
    role="switch"
    :aria-checked="checked"
    :aria-label="label"
    :title="label"
    :disabled="disabled"
    class="relative inline-flex h-5 w-9 shrink-0 items-center rounded-full transition-colors duration-150 disabled:opacity-45"
    :class="
      checked
        ? 'bg-brand-600 hover:bg-brand-700'
        : 'bg-field shadow-[inset_0_0_0_1px_var(--color-gray-500)] hover:shadow-[inset_0_0_0_1px_var(--color-gray-700)]'
    "
    @click="checked = !checked"
  >
    <span
      class="size-3.5 rounded-full transition-transform duration-150 ease-[var(--ease-decelerate)]"
      :class="
        checked
          ? 'translate-x-[19px] bg-white'
          : 'translate-x-[3px] bg-gray-600'
      "
    />
  </button>
</template>
