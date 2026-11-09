<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import {
  Bell,
  CircleAlert,
  LayoutGrid,
  LogOut,
  Radar,
  RefreshCw,
  SlidersHorizontal,
  TriangleAlert,
} from 'lucide-vue-next'
import AdminOverview from './components/AdminOverview.vue'
import MatchesView from './components/MatchesView.vue'
import SearchEditor from './components/SearchEditor.vue'
import SearchesView from './components/SearchesView.vue'
import PreferencesMenu from './components/PreferencesMenu.vue'
import { api, ApiError, type Me } from './api'
import { t } from './i18n'
import { onLinkClick, route } from './router'

const me = ref<Me | null>(null)
const state = ref<'loading' | 'out' | 'in' | 'down'>('loading')
// The sidebar entry for the current page; an editor belongs to Searches.
const tab = computed(() =>
  route.value.name === 'search' ? 'searches' : route.value.name,
)
const expired = new URLSearchParams(location.search).get('login') === 'expired'

const navigation = [
  {
    id: 'searches' as const,
    href: '/searches',
    label: 'app.searches' as const,
    icon: SlidersHorizontal,
  },
  {
    id: 'alerts' as const,
    href: '/alerts',
    label: 'app.alerts' as const,
    icon: Bell,
  },
]
const adminNavigation = [
  {
    id: 'overview' as const,
    href: '/overview',
    label: 'app.overview' as const,
    icon: LayoutGrid,
  },
]
const initials = computed(() =>
  (me.value?.name ?? '')
    .split(/[\s._-]+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((part) => part[0]?.toUpperCase())
    .join(''),
)

onMounted(async () => {
  try {
    me.value = await api.me()
    state.value = 'in'
  } catch (reason) {
    state.value =
      reason instanceof ApiError && reason.status === 401 ? 'out' : 'down'
  }
  if (expired) history.replaceState(null, '', '/')
})

function reload() {
  location.reload()
}

async function logout() {
  await api.logout().catch(() => undefined)
  me.value = null
  state.value = 'out'
}
</script>

<template>
  <!-- Signed in: sidebar on wide screens, top bar on narrow ones. -->
  <div v-if="state === 'in' && me" class="min-h-screen lg:flex">
    <aside
      class="sticky top-0 z-20 hidden h-screen w-64 shrink-0 flex-col border-r border-gray-150 bg-gray-100/70 lg:flex"
    >
      <a
        href="/searches"
        class="flex h-16 items-center gap-2.5 px-5"
        @click="onLinkClick($event, '/searches')"
      >
        <span
          class="grid size-7 place-items-center rounded-md bg-brand-600 text-white shadow-[0_1px_2px_rgb(15_108_189/0.3)]"
          ><Radar :size="16"
        /></span>
        <span class="text-sm font-semibold text-gray-900">{{
          t('app.name')
        }}</span>
      </a>

      <nav :aria-label="t('app.mainNav')" class="flex-1 px-3 pt-2">
        <template
          v-for="group in me.admin
            ? [
                { label: '', items: navigation },
                { label: t('app.administration'), items: adminNavigation },
              ]
            : [{ label: '', items: navigation }]"
          :key="group.label"
        >
          <p
            v-if="group.label"
            class="mt-6 mb-1.5 px-3 text-xs font-medium text-gray-500"
          >
            {{ group.label }}
          </p>
          <a
            v-for="item in group.items"
            :href="item.href"
            :key="item.id"
            :aria-current="tab === item.id ? 'page' : undefined"
            class="relative mb-0.5 flex h-9 w-full items-center gap-3 rounded-md px-3 text-sm transition-colors duration-100"
            :class="
              tab === item.id
                ? 'bg-raised font-medium text-gray-900 shadow-card'
                : 'text-gray-700 hover:bg-gray-150/70 hover:text-gray-900'
            "
            @click="onLinkClick($event, item.href)"
          >
            <span
              v-if="tab === item.id"
              class="absolute top-1/2 left-0 h-4 w-[3px] -translate-y-1/2 rounded-full bg-brand-600"
              aria-hidden="true"
            />
            <component
              :is="item.icon"
              :size="18"
              :class="tab === item.id ? 'text-brand-600' : 'text-gray-500'"
            />{{ t(item.label) }}
          </a>
        </template>
      </nav>

      <div class="flex items-center gap-3 border-t border-gray-150 px-4 py-3">
        <span
          class="grid size-8 shrink-0 place-items-center rounded-full bg-brand-100 text-xs font-semibold text-brand-700"
          aria-hidden="true"
          >{{ initials }}</span
        >
        <div class="min-w-0 flex-1">
          <p class="truncate text-sm font-medium text-gray-900">
            {{ me.name }}
          </p>
          <p class="text-xs text-gray-500">
            {{ me.admin ? t('app.administrator') : t('app.member') }}
          </p>
        </div>
        <PreferencesMenu placement="up" />
        <button
          class="btn-icon -ml-1"
          :aria-label="t('app.signOut')"
          :title="t('app.signOut')"
          @click="logout"
        >
          <LogOut :size="16" />
        </button>
      </div>
    </aside>

    <header
      class="sticky top-0 z-10 border-b border-gray-150 bg-surface/90 backdrop-blur-md lg:hidden"
    >
      <div class="flex h-14 items-center gap-2.5 px-4">
        <span
          class="grid size-7 place-items-center rounded-md bg-brand-600 text-white"
          ><Radar :size="16"
        /></span>
        <span class="flex-1 text-sm font-semibold">{{ t('app.name') }}</span>
        <PreferencesMenu />
        <button
          class="btn-icon"
          :aria-label="t('app.signOut')"
          :title="t('app.signOut')"
          @click="logout"
        >
          <LogOut :size="16" />
        </button>
      </div>
      <nav
        :aria-label="t('app.mainNav')"
        class="flex gap-5 overflow-x-auto px-4"
      >
        <a
          v-for="item in me.admin
            ? [...navigation, ...adminNavigation]
            : navigation"
          :href="item.href"
          :key="item.id"
          :aria-current="tab === item.id ? 'page' : undefined"
          class="relative flex h-10 shrink-0 items-center text-sm transition-colors"
          :class="
            tab === item.id
              ? 'font-medium text-gray-900'
              : 'text-gray-600 hover:text-gray-900'
          "
          @click="onLinkClick($event, item.href)"
        >
          {{ t(item.label) }}
          <span
            v-if="tab === item.id"
            class="absolute inset-x-0 bottom-0 h-[3px] rounded-full bg-brand-600"
            aria-hidden="true"
          />
        </a>
      </nav>
    </header>

    <main class="min-w-0 flex-1">
      <div class="mx-auto max-w-[1200px] px-4 py-8 sm:px-8 lg:px-10 lg:py-10">
        <SearchEditor v-if="route.name === 'search'" :me="me" :id="route.id" />
        <MatchesView v-else-if="route.name === 'alerts'" />
        <AdminOverview v-else-if="route.name === 'overview' && me.admin" />
        <SearchesView v-else :me="me" />
      </div>
    </main>
  </div>

  <!-- Signed out, unreachable or still checking: a single centered card. -->
  <main
    v-else
    class="grid min-h-screen place-items-center bg-gray-50 px-4 py-16"
    :aria-busy="state === 'loading'"
  >
    <div
      v-if="state === 'loading'"
      class="size-6 animate-spin rounded-full border-2 border-gray-200 border-t-brand-600"
      role="status"
      :aria-label="t('common.loading')"
    />

    <div v-else class="w-full max-w-[400px]">
      <div class="fixed top-3 right-3">
        <PreferencesMenu />
      </div>
      <div class="mb-6 flex items-center justify-center gap-2.5">
        <span
          class="grid size-8 place-items-center rounded-lg bg-brand-600 text-white shadow-[0_1px_2px_rgb(15_108_189/0.3)]"
          ><Radar :size="18"
        /></span>
        <span class="text-base font-semibold">{{ t('app.name') }}</span>
      </div>

      <section v-if="state === 'out'" class="card p-8">
        <h1 class="text-title font-semibold">{{ t('signIn.title') }}</h1>
        <p class="mt-1 text-sm text-gray-600">
          {{ t('signIn.lead') }}
        </p>
        <div v-if="expired" role="alert" class="message-bar-warning mt-5">
          <TriangleAlert :size="16" class="mt-0.5 shrink-0 text-warning-600" />
          {{ t('signIn.expired') }}
        </div>
        <ol class="mt-6 grid gap-4 text-sm">
          <li class="flex gap-3">
            <span
              class="grid size-6 shrink-0 place-items-center rounded-full bg-gray-100 text-xs font-semibold text-gray-700"
              >1</span
            >
            <span class="pt-0.5 text-gray-700"
              >{{ t('signIn.step1Before') }}
              <span class="kbd">/dashboard</span>
              {{ t('signIn.step1After') }}</span
            >
          </li>
          <li class="flex gap-3">
            <span
              class="grid size-6 shrink-0 place-items-center rounded-full bg-gray-100 text-xs font-semibold text-gray-700"
              >2</span
            >
            <span class="pt-0.5 text-gray-700">{{ t('signIn.step2') }}</span>
          </li>
        </ol>
        <p class="mt-6 border-t border-gray-150 pt-4 text-xs text-gray-500">
          {{ t('signIn.footnote') }}
        </p>
      </section>

      <section v-else role="alert" class="card p-8 text-center">
        <span
          class="mx-auto mb-4 grid size-10 place-items-center rounded-full bg-danger-50 text-danger-600"
          ><CircleAlert :size="20"
        /></span>
        <h1 class="text-base font-semibold">{{ t('down.title') }}</h1>
        <p class="mt-1 text-sm text-gray-600">
          {{ t('down.body') }}
        </p>
        <button class="btn-secondary mt-6" @click="reload">
          <RefreshCw :size="14" />{{ t('common.tryAgain') }}
        </button>
      </section>
    </div>
  </main>
</template>
