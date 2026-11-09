<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ArrowUpRight, Bell, CircleAlert, ImageOff } from 'lucide-vue-next'
import { api, ApiError, type Match } from '../api'

const matches = ref<Match[]>([])
const error = ref('')
const loading = ref(true)
const broken = ref(new Set<string>())

function when(at: string) {
  return new Date(at).toLocaleString([], {
    day: 'numeric',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
  })
}

onMounted(async () => {
  try {
    matches.value = await api.matches(100)
  } catch (reason) {
    error.value =
      reason instanceof ApiError
        ? reason.message
        : 'Could not load your alerts.'
  } finally {
    loading.value = false
  }
})
</script>

<template>
  <section>
    <h1 class="mb-1 text-2xl font-semibold text-zinc-900">Your alerts</h1>
    <p class="mb-6 text-sm text-zinc-500">
      The latest ads your searches told you about.
    </p>

    <div
      v-if="error"
      role="alert"
      class="mb-5 flex items-start gap-2 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-800"
    >
      <CircleAlert :size="16" class="mt-0.5 shrink-0" />{{ error }}
    </div>
    <p v-if="loading" class="text-sm text-zinc-500">Loading…</p>
    <div
      v-else-if="!matches.length"
      class="rounded-lg border border-dashed border-zinc-300 bg-white p-10 text-center"
    >
      <Bell :size="28" class="mx-auto mb-3 text-zinc-400" />
      <p class="font-medium">No alerts yet</p>
      <p class="mt-1 text-sm text-zinc-500">
        They appear here as new matching ads are posted.
      </p>
    </div>

    <ul class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
      <li
        v-for="m in matches"
        :key="`${m.searchId}-${m.externalId}`"
        class="flex gap-3 rounded-lg border border-zinc-200 bg-white p-3"
      >
        <div
          class="grid size-20 shrink-0 place-items-center overflow-hidden rounded bg-zinc-100"
        >
          <img
            v-if="m.listing?.imageUrl && !broken.has(m.externalId)"
            :src="m.listing.imageUrl"
            alt=""
            class="size-full object-cover"
            loading="lazy"
            @error="broken.add(m.externalId)"
          />
          <ImageOff v-else :size="18" class="text-zinc-400" />
        </div>
        <div class="min-w-0 flex-1">
          <p class="text-xs text-zinc-500">
            {{ m.searchName }} · {{ when(m.at) }}
          </p>
          <template v-if="m.listing">
            <p class="mt-1 line-clamp-2 text-sm text-zinc-800">
              {{ m.listing.title }}
            </p>
            <div class="mt-1.5 flex items-center justify-between gap-2">
              <span class="font-semibold">{{ m.listing.price }}</span>
              <a
                :href="m.listing.url"
                target="_blank"
                rel="noopener noreferrer"
                class="inline-flex items-center gap-1 text-xs text-accent-600 hover:text-accent-700"
                >Open<ArrowUpRight :size="12"
              /></a>
            </div>
          </template>
          <p v-else class="mt-1 text-sm text-zinc-400">
            Ad no longer stored ({{ m.externalId }})
          </p>
        </div>
      </li>
    </ul>
  </section>
</template>
