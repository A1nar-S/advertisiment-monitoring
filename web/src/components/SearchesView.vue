<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import {
  CircleAlert,
  Hash,
  Pencil,
  Plus,
  SlidersHorizontal,
  Trash2,
} from 'lucide-vue-next'
import BaseDialog from './BaseDialog.vue'
import EmptyState from './EmptyState.vue'
import PageHeader from './PageHeader.vue'
import ToggleSwitch from './ToggleSwitch.vue'
import {
  api,
  ApiError,
  describe,
  type Category,
  type Me,
  type Search,
} from '../api'
import { categoryName, formatNumber, t } from '../i18n'
import { navigate, onLinkClick } from '../router'
import { showEveryone as everyone } from '../state'

const props = defineProps<{ me: Me }>()

const searches = ref<Search[]>([])
const categories = ref<Category[]>([])
const deleting = ref<Search | null>(null)
const error = ref('')
const loading = ref(true)

const atLimit = computed(
  () =>
    !props.me.admin &&
    props.me.maxSearches > 0 &&
    searches.value.length >= props.me.maxSearches,
)

const fieldsOf = (category: string) =>
  categories.value.find((c) => c.id === category)?.fields
const categoryLabel = (id: string) =>
  categoryName(id, categories.value.find((c) => c.id === id)?.label ?? id)

async function load() {
  error.value = ''
  try {
    ;[searches.value, categories.value] = await Promise.all([
      api.searches(everyone.value),
      api.categories(),
    ])
  } catch (reason) {
    error.value =
      reason instanceof ApiError ? reason.message : t('searches.loadFailed')
  } finally {
    loading.value = false
  }
}

async function act(fn: () => Promise<void>) {
  error.value = ''
  try {
    await fn()
  } catch (reason) {
    error.value =
      reason instanceof ApiError ? reason.message : t('searches.actionFailed')
  }
  await load()
}

function setEnabled(s: Search, enabled: boolean) {
  s.enabled = enabled
  void act(() => api.setEnabled(s.id, enabled))
}

function confirmDelete() {
  const s = deleting.value
  deleting.value = null
  if (s) void act(() => api.deleteSearch(s.id))
}

function showEveryone(value: boolean) {
  if (everyone.value === value) return
  everyone.value = value
  loading.value = true
  void load()
}

onMounted(load)
</script>

<template>
  <section>
    <PageHeader :title="t('searches.title')">
      <template #description>
        {{ t('searches.lead') }}
        <template v-if="me.maxSearches && !me.admin">
          <span class="text-gray-400"> · </span
          ><span class="tabular-nums">{{
            t('searches.used', { used: searches.length, max: me.maxSearches })
          }}</span></template
        >
      </template>
      <div
        v-if="me.admin"
        class="flex rounded-md bg-gray-150/80 p-0.5"
        role="group"
        :aria-label="t('searches.whose')"
      >
        <button
          v-for="option in [
            { value: false, label: t('searches.mine') },
            { value: true, label: t('searches.everyone') },
          ]"
          :key="option.label"
          :aria-pressed="everyone === option.value"
          class="h-7 rounded-[5px] px-3 text-small transition-colors"
          :class="
            everyone === option.value
              ? 'bg-raised font-medium text-gray-900 shadow-card'
              : 'text-gray-600 hover:text-gray-900'
          "
          @click="showEveryone(option.value)"
        >
          {{ option.label }}
        </button>
      </div>
      <button
        class="btn-primary"
        :disabled="atLimit"
        :title="atLimit ? t('searches.atLimit') : undefined"
        @click="navigate('/searches/new')"
      >
        <Plus :size="16" />{{ t('searches.new') }}
      </button>
    </PageHeader>

    <div v-if="error" role="alert" class="message-bar-danger mb-6">
      <CircleAlert :size="16" class="mt-0.5 shrink-0 text-danger-600" />{{
        error
      }}
    </div>

    <div class="card overflow-hidden">
      <div
        v-if="loading"
        class="divide-y divide-gray-100"
        role="status"
        :aria-label="t('searches.loading')"
      >
        <div v-for="n in 3" :key="n" class="flex items-center gap-6 px-5 py-4">
          <div class="flex-1">
            <div class="skeleton h-4 w-48" />
            <div class="skeleton mt-2 h-3 w-80 max-w-full" />
          </div>
          <div class="skeleton h-5 w-9 rounded-full" />
        </div>
      </div>

      <EmptyState
        v-else-if="!searches.length"
        :icon="SlidersHorizontal"
        :title="
          everyone ? t('searches.emptyEveryoneTitle') : t('searches.emptyTitle')
        "
        :description="t('searches.emptyBody')"
      >
        <button class="btn-primary" @click="navigate('/searches/new')">
          <Plus :size="16" />{{ t('searches.new') }}
        </button>
      </EmptyState>

      <template v-else>
        <div
          class="hidden grid-cols-[minmax(0,1fr)_200px_72px_64px_80px] items-center border-b border-gray-150 bg-gray-25 px-4 md:grid"
        >
          <span class="table-head flex items-center pl-0!">{{
            t('searches.colSearch')
          }}</span>
          <span class="table-head flex items-center">{{
            t('searches.colSendsTo')
          }}</span>
          <span class="table-head flex items-center justify-end">{{
            t('searches.colAlerts')
          }}</span>
          <span class="table-head flex items-center">{{
            t('searches.colActive')
          }}</span>
          <span class="table-head"
            ><span class="sr-only">{{ t('searches.colActions') }}</span></span
          >
        </div>
        <ul class="divide-y divide-gray-100">
          <li
            v-for="s in searches"
            :key="s.id"
            class="group grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-4 gap-y-3 px-4 py-3.5 transition-colors hover:bg-gray-25 md:grid-cols-[minmax(0,1fr)_200px_72px_64px_80px] md:gap-0 md:py-3"
          >
            <div class="min-w-0 md:px-4 md:pl-0">
              <div class="flex min-w-0 items-center gap-2">
                <a
                  :href="`/searches/${s.id}`"
                  class="truncate font-medium hover:text-brand-700 hover:underline hover:underline-offset-2"
                  :class="s.enabled ? 'text-gray-900' : 'text-gray-500'"
                  @click="onLinkClick($event, `/searches/${s.id}`)"
                  >{{ s.name }}</a
                >
                <span class="badge-neutral">{{
                  categoryLabel(s.category)
                }}</span>
                <span v-if="!s.enabled" class="badge-warning">{{
                  t('common.paused')
                }}</span>
              </div>
              <p
                class="mt-0.5 truncate text-small text-gray-500"
                :title="describe(s.filters, fieldsOf(s.category))"
              >
                <span v-if="everyone" class="text-gray-700"
                  >{{ s.userName }} ·
                </span>
                {{ describe(s.filters, fieldsOf(s.category)) }}
              </p>
            </div>

            <div
              class="col-span-2 row-start-2 flex min-w-0 items-center gap-1 text-small text-gray-600 md:col-span-1 md:row-start-auto md:px-4"
            >
              <Hash :size="14" class="shrink-0 text-gray-400" />
              <span class="truncate">{{
                s.channel.label || s.channel.target
              }}</span>
              <span class="ml-auto text-gray-500 tabular-nums md:hidden">{{
                t('searches.alertCount', { n: s.matches })
              }}</span>
            </div>

            <span
              class="hidden px-4 text-right text-sm text-gray-900 tabular-nums md:block"
              >{{ formatNumber(s.matches) }}</span
            >

            <div
              class="col-start-2 row-start-1 flex items-center gap-1 md:contents"
            >
              <div class="md:px-4">
                <ToggleSwitch
                  :model-value="s.enabled"
                  :label="
                    s.enabled
                      ? t('searches.pause', { name: s.name })
                      : t('searches.resume', { name: s.name })
                  "
                  @update:model-value="setEnabled(s, $event)"
                />
              </div>
              <div class="flex items-center justify-end gap-0.5">
                <a
                  class="btn-icon"
                  :href="`/searches/${s.id}`"
                  :aria-label="t('searches.editNamed', { name: s.name })"
                  :title="t('common.edit')"
                  @click="onLinkClick($event, `/searches/${s.id}`)"
                >
                  <Pencil :size="16" />
                </a>
                <button
                  class="btn-icon hover:bg-danger-50 hover:text-danger-600"
                  :aria-label="t('searches.deleteNamed', { name: s.name })"
                  :title="t('common.delete')"
                  @click="deleting = s"
                >
                  <Trash2 :size="16" />
                </button>
              </div>
            </div>
          </li>
        </ul>
      </template>
    </div>

    <BaseDialog
      v-if="deleting"
      variant="modal"
      :title="t('searches.deleteTitle')"
      light-dismiss
      @close="deleting = null"
    >
      <p class="text-sm text-gray-600">
        {{ t('searches.deleteBody', { name: deleting.name }) }}
      </p>
      <template #footer>
        <button class="btn-secondary" @click="deleting = null">
          {{ t('common.cancel') }}
        </button>
        <button class="btn-danger" @click="confirmDelete">
          {{ t('common.delete') }}
        </button>
      </template>
    </BaseDialog>
  </section>
</template>
