<script setup lang="ts">
import type { Component } from 'vue'

defineProps<{ icon: Component; title: string; description?: string }>()
</script>

<template>
  <div class="flex flex-col items-center px-6 py-16 text-center">
    <span
      class="mb-4 grid size-11 place-items-center rounded-full bg-gray-100 text-gray-500"
      ><component :is="icon" :size="20"
    /></span>
    <h3 class="text-base font-semibold text-gray-900">{{ title }}</h3>
    <p v-if="description" class="mt-1 max-w-sm text-sm text-gray-600">
      {{ description }}
    </p>
    <div v-if="$slots.default" class="mt-5">
      <slot />
    </div>
  </div>
</template>
