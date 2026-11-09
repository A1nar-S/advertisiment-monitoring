// Typed client for the Go API (internal/web). The session is an HttpOnly
// cookie; every change carries X-Requested-With, which the API requires.

export type Me = {
  name: string
  platform: string
  admin: boolean
  maxSearches: number
}

export type FieldSpec = {
  key: string
  label: string
  kind: 'multi' | 'all' | 'range' | 'text'
  required?: boolean
  detail?: boolean
  options?: string[]
  dependsOn?: string
  optionsBy?: Record<string, string[]>
}

export type Category = { id: string; label: string; fields: FieldSpec[] }

export type Channel = {
  id: number
  kind: string
  target: string
  label: string
}

export type Range = { min?: number; max?: number }
export type Filters = Record<string, string[] | Range>

export type Search = {
  id: number
  userId: number
  userName: string
  name: string
  enabled: boolean
  source: string
  category: string
  filters: Filters
  feeds: string[]
  channel: Channel
  matches: number
  createdAt: string
  updatedAt: string
}

export type SearchInput = {
  name: string
  category: string
  filters: Filters
  channelId: number
}

export type Match = {
  searchId: number
  searchName: string
  source: string
  externalId: string
  at: string
  listing?: {
    title: string
    url: string
    price: string
    imageUrl: string
    attrs: Record<string, unknown>
    active: boolean
  }
}

export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message)
  }
}

async function call<T>(
  method: string,
  path: string,
  body?: unknown,
): Promise<T> {
  const response = await fetch(path, {
    method,
    credentials: 'same-origin',
    headers: {
      'X-Requested-With': 'fetch',
      ...(body === undefined ? {} : { 'Content-Type': 'application/json' }),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (response.status === 204) return undefined as T
  const data = await response.json().catch(() => ({}))
  if (!response.ok)
    throw new ApiError(
      response.status,
      data.error ?? `Request failed (${response.status})`,
    )
  return data as T
}

export const api = {
  me: () => call<Me>('GET', '/api/me'),
  logout: () => call<void>('POST', '/api/logout'),
  categories: () => call<Category[]>('GET', '/api/categories'),
  channels: () => call<Channel[]>('GET', '/api/channels'),
  searches: (all = false) =>
    call<Search[]>('GET', `/api/searches${all ? '?all=1' : ''}`),
  createSearch: (input: SearchInput) =>
    call<Search>('POST', '/api/searches', input),
  updateSearch: (id: number, input: SearchInput) =>
    call<Search>('PUT', `/api/searches/${id}`, input),
  setEnabled: (id: number, enabled: boolean) =>
    call<void>('PATCH', `/api/searches/${id}`, { enabled }),
  deleteSearch: (id: number) => call<void>('DELETE', `/api/searches/${id}`),
  matches: (limit = 50) => call<Match[]>('GET', `/api/matches?limit=${limit}`),
}

// describe renders filters as one line: "make Skoda, Toyota · price ≤ 7000".
export function describe(filters: Filters, fields?: FieldSpec[]): string {
  const label = (key: string) =>
    fields?.find((f) => f.key === key)?.label ?? key
  const parts = Object.keys(filters)
    .sort()
    .map((key) => {
      const value = filters[key]
      if (Array.isArray(value)) return `${label(key)}: ${value.join(', ')}`
      if (value.min !== undefined && value.max !== undefined)
        return `${label(key)} ${value.min}–${value.max}`
      if (value.min !== undefined) return `${label(key)} ≥ ${value.min}`
      if (value.max !== undefined) return `${label(key)} ≤ ${value.max}`
      return ''
    })
    .filter(Boolean)
  return parts.length ? parts.join(' · ') : 'Everything'
}
