<script setup lang="ts">
defineProps<{ title: string; description?: string }>()
</script>

<template>
  <header class="mb-8 flex flex-wrap items-end justify-between gap-x-6 gap-y-4">
    <div class="min-w-0">
      <h1 class="text-display font-semibold tracking-[-0.015em] text-gray-900">
        {{ title }}
      </h1>
      <p
        v-if="description || $slots.description"
        class="mt-1 text-sm text-gray-600"
      >
        <slot name="description">{{ description }}</slot>
      </p>
    </div>
    <div v-if="$slots.default" class="flex flex-wrap items-center gap-2">
      <slot />
    </div>
  </header>
</template>
