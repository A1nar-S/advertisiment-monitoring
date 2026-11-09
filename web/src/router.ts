import { computed, ref } from 'vue'

// A tiny history router. The API serves index.html for any unknown path, so
// /searches/12 survives a refresh and the browser's Back button works.

export type Route =
  | { name: 'searches' }
  | { name: 'search'; id: number | 'new' }
  | { name: 'alerts' }
  | { name: 'overview' }

const path = ref(location.pathname)

export const route = computed<Route>(() => {
  const parts = path.value.split('/').filter(Boolean)
  if (parts[0] === 'alerts') return { name: 'alerts' }
  if (parts[0] === 'overview') return { name: 'overview' }
  if (parts[0] === 'searches' && parts[1] === 'new')
    return { name: 'search', id: 'new' }
  if (parts[0] === 'searches' && /^\d+$/.test(parts[1] ?? ''))
    return { name: 'search', id: Number(parts[1]) }
  return { name: 'searches' }
})

// A page with unsaved work can hold navigation: it gets `proceed` and
// returns false to stop here (e.g. to ask first, then call proceed).
type Guard = (proceed: () => void) => boolean
let guard: Guard | null = null

export function setLeaveGuard(next: Guard | null) {
  guard = next
}

export function navigate(to: string, { replace = false } = {}) {
  const go = () => {
    if (to === path.value) return
    if (replace) history.replaceState(null, '', to)
    else history.pushState(null, '', to)
    path.value = to
    window.scrollTo(0, 0)
  }
  if (guard && !guard(go)) return
  go()
}

// Browser Back/Forward. A blocked guard puts the page's address back.
window.addEventListener('popstate', () => {
  const to = location.pathname
  const from = path.value
  if (guard && !guard(() => navigate(to))) {
    history.pushState(null, '', from)
    return
  }
  path.value = to
})

// For <a href> links: plain clicks go through the router, while ctrl/cmd or
// middle clicks still open a new tab.
export function onLinkClick(event: MouseEvent, to: string) {
  if (event.defaultPrevented || event.button !== 0) return
  if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
  event.preventDefault()
  navigate(to)
}
