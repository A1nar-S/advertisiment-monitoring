<script setup lang="ts">
import { onMounted, ref } from 'vue'
import {
  Bell,
  CircleAlert,
  LayoutDashboard,
  LogOut,
  Radio,
  SearchCheck,
} from 'lucide-vue-next'
import AdminOverview from './components/AdminOverview.vue'
import MatchesView from './components/MatchesView.vue'
import SearchesView from './components/SearchesView.vue'
import { api, ApiError, type Me } from './api'

type Tab = 'searches' | 'alerts' | 'overview'

const me = ref<Me | null>(null)
const state = ref<'loading' | 'out' | 'in' | 'down'>('loading')
const tab = ref<Tab>('searches')
const expired = new URLSearchParams(location.search).get('login') === 'expired'

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

async function logout() {
  await api.logout().catch(() => undefined)
  me.value = null
  state.value = 'out'
}
</script>

<template>
  <div class="min-h-screen">
    <header class="border-b border-zinc-200 bg-white">
      <div
        class="mx-auto flex h-16 max-w-[1328px] items-center justify-between gap-4 px-5 sm:px-8 lg:px-12"
      >
        <a
          href="/"
          aria-label="Ad Monitor home"
          class="flex items-center gap-2.5"
          ><span
            class="grid size-8 place-items-center rounded-md bg-accent-600 text-white"
            ><Radio :size="20" :stroke-width="1.8" /></span
          ><span class="text-lg font-semibold"
            >admonitor<span class="text-accent-600">.</span></span
          ></a
        >
        <div v-if="me" class="flex items-center gap-3 text-sm">
          <span class="text-zinc-600">{{ me.name }}</span>
          <button
            class="icon-button"
            aria-label="Log out"
            title="Log out"
            @click="logout"
          >
            <LogOut :size="16" />
          </button>
        </div>
      </div>
      <nav
        v-if="me"
        aria-label="Main navigation"
        class="mx-auto flex max-w-[1328px] items-center gap-7 px-5 sm:px-8 lg:px-12"
      >
        <button
          v-for="item in [
            { id: 'searches' as const, label: 'Searches', icon: SearchCheck },
            { id: 'alerts' as const, label: 'Alerts', icon: Bell },
            ...(me.admin
              ? [
                  {
                    id: 'overview' as const,
                    label: 'Overview',
                    icon: LayoutDashboard,
                  },
                ]
              : []),
          ]"
          :key="item.id"
          :aria-current="tab === item.id ? 'page' : undefined"
          class="-mb-px flex h-11 items-center gap-2 border-b-2 text-sm transition-colors"
          :class="
            tab === item.id
              ? 'border-accent-600 font-semibold text-zinc-900'
              : 'border-transparent text-zinc-500 hover:text-zinc-900'
          "
          @click="tab = item.id"
        >
          <component :is="item.icon" :size="16" />{{ item.label }}
        </button>
      </nav>
    </header>

    <main
      v-if="state === 'in' && me && tab !== 'overview'"
      class="mx-auto max-w-[1100px] px-5 py-9 sm:px-8"
    >
      <SearchesView v-if="tab === 'searches'" :me="me" />
      <MatchesView v-else />
    </main>
    <AdminOverview v-else-if="state === 'in' && tab === 'overview'" />

    <main
      v-else-if="state === 'out'"
      class="mx-auto max-w-md px-5 py-20 text-center"
    >
      <h1 class="text-2xl font-semibold">Log in from Discord</h1>
      <p v-if="expired" class="mt-3 text-sm text-amber-700">
        That login link has expired or was already used.
      </p>
      <p class="mt-3 text-sm text-zinc-600">
        Type
        <code class="rounded bg-zinc-100 px-1.5 py-0.5">/dashboard</code> in a
        server where the bot is, and open the link it gives you. Links work once
        and for 10 minutes.
      </p>
    </main>

    <main v-else-if="state === 'down'" class="mx-auto max-w-md px-5 py-20">
      <div
        role="alert"
        class="flex items-start gap-2 rounded-md border border-red-200 bg-red-50 p-4 text-sm text-red-800"
      >
        <CircleAlert :size="16" class="mt-0.5 shrink-0" />The dashboard can't
        reach its API right now. Try again in a moment.
      </div>
    </main>
  </div>
</template>
