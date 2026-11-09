<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ArrowUpRight, Bell, CircleAlert, ImageOff } from 'lucide-vue-next'
import EmptyState from './EmptyState.vue'
import PageHeader from './PageHeader.vue'
import { api, ApiError, type Match } from '../api'

const matches = ref<Match[]>([])
const error = ref('')
const loading = ref(true)
const broken = ref(new Set<string>())

const dayKey = (at: string) => new Date(at).toDateString()

function dayLabel(at: string) {
  const date = new Date(at)
  const today = new Date()
  const yesterday = new Date()
  yesterday.setDate(today.getDate() - 1)
  if (date.toDateString() === today.toDateString()) return 'Today'
  if (date.toDateString() === yesterday.toDateString()) return 'Yesterday'
  return date.toLocaleDateString([], {
    weekday: 'long',
    day: 'numeric',
    month: 'long',
    year: date.getFullYear() === today.getFullYear() ? undefined : 'numeric',
  })
}

const time = (at: string) =>
  new Date(at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })

// Matches arrive newest first; group them under one heading per day.
const days = computed(() => {
  const groups: { key: string; label: string; items: Match[] }[] = []
  for (const m of matches.value) {
    const key = dayKey(m.at)
    const last = groups.at(-1)
    if (last?.key === key) last.items.push(m)
    else groups.push({ key, label: dayLabel(m.at), items: [m] })
  }
  return groups
})

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
    <PageHeader
      title="Alerts"
      description="The latest ads your searches found, newest first."
    />

    <div v-if="error" role="alert" class="message-bar-danger mb-6">
      <CircleAlert :size="16" class="mt-0.5 shrink-0 text-danger-600" />{{
        error
      }}
    </div>

    <div class="card overflow-clip">
      <div
        v-if="loading"
        class="divide-y divide-gray-100"
        role="status"
        aria-label="Loading alerts"
      >
        <div v-for="n in 4" :key="n" class="flex items-center gap-4 px-4 py-3">
          <div class="skeleton h-12 w-16 rounded-md" />
          <div class="flex-1">
            <div class="skeleton h-4 w-72 max-w-full" />
            <div class="skeleton mt-2 h-3 w-40" />
          </div>
          <div class="skeleton h-4 w-16" />
        </div>
      </div>

      <EmptyState
        v-else-if="!matches.length"
        :icon="Bell"
        title="No alerts yet"
        description="When a new ad matches one of your searches, it shows up here and in Discord."
      />

      <template v-else>
        <section
          v-for="day in days"
          :key="day.key"
          :aria-label="day.label"
          class="not-first:border-t not-first:border-gray-150"
        >
          <h2
            class="sticky top-24 z-[1] lg:top-0 border-b border-gray-150 bg-gray-25/95 px-4 py-2 font-sans text-xs font-medium text-gray-500 backdrop-blur"
          >
            {{ day.label }}
          </h2>
          <ul class="divide-y divide-gray-100">
            <li v-for="m in day.items" :key="`${m.searchId}-${m.externalId}`">
              <component
                :is="m.listing ? 'a' : 'div'"
                :href="m.listing?.url"
                :target="m.listing ? '_blank' : undefined"
                :rel="m.listing ? 'noopener noreferrer' : undefined"
                class="group flex items-center gap-4 px-4 py-3"
                :class="{ 'transition-colors hover:bg-gray-25': m.listing }"
              >
                <div
                  class="grid h-12 w-16 shrink-0 place-items-center overflow-hidden rounded-md bg-gray-100 text-gray-400"
                >
                  <img
                    v-if="m.listing?.imageUrl && !broken.has(m.externalId)"
                    :src="m.listing.imageUrl"
                    alt=""
                    class="size-full object-cover"
                    loading="lazy"
                    referrerpolicy="no-referrer"
                    @error="broken.add(m.externalId)"
                  />
                  <ImageOff v-else :size="16" />
                </div>

                <div class="min-w-0 flex-1">
                  <p
                    class="truncate text-sm"
                    :class="m.listing ? 'text-gray-900' : 'text-gray-500'"
                  >
                    {{ m.listing?.title ?? 'This ad is no longer stored' }}
                  </p>
                  <p
                    class="mt-0.5 truncate text-small text-gray-500 tabular-nums"
                  >
                    {{
                      [
                        m.searchName,
                        time(m.at),
                        m.listing && !m.listing.active
                          ? 'No longer listed'
                          : '',
                      ]
                        .filter(Boolean)
                        .join(' · ')
                    }}
                  </p>
                </div>

                <span
                  v-if="m.listing?.price"
                  class="shrink-0 text-sm font-semibold text-gray-900 tabular-nums"
                  >{{ m.listing.price }}</span
                >
                <ArrowUpRight
                  v-if="m.listing"
                  :size="16"
                  class="shrink-0 text-gray-400 transition-colors group-hover:text-brand-600"
                  aria-hidden="true"
                />
              </component>
            </li>
          </ul>
        </section>
      </template>
    </div>
  </section>
</template>
