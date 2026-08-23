import type {
  AuthResponse,
  DashboardMetricsResponse,
  ListSEVsParams,
  ListSEVsResponse,
  SEVResponse,
  WhoAmIResponse,
} from '@/types/api'

/** Thrown for any non-2xx response. `status` lets callers special-case 401
 * (session expired/invalid — see AuthContext) without string-matching. */
export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

const TOKEN_STORAGE_KEY = 'sevitout.token'

/** Reads/writes the JWT to localStorage. The backend's /auth/login only
 * returns the token in the JSON body (no httpOnly cookie is set — see
 * internal/auth/password.go) so, unlike the httpOnly-cookie flow sketched in
 * docs/architecture.md §9, the token is held here and sent explicitly as an
 * `Authorization: Bearer` header on every request. This is the same header
 * internal/auth/interceptor.go and the gRPC-gateway's WithMetadata option
 * already expect, and it's what every other client of this API (the Slack
 * bot, curl in the demo docs) does too. */
export const tokenStorage = {
  get(): string | null {
    try {
      return localStorage.getItem(TOKEN_STORAGE_KEY)
    } catch {
      return null
    }
  },
  set(token: string) {
    try {
      localStorage.setItem(TOKEN_STORAGE_KEY, token)
    } catch {
      // Storage unavailable (private browsing, quota) — the session simply
      // won't survive a reload; not fatal.
    }
  },
  clear() {
    try {
      localStorage.removeItem(TOKEN_STORAGE_KEY)
    } catch {
      // ignore
    }
  },
}

/** Called whenever a request comes back 401 — AuthContext wires this to log
 * the user out and redirect to /login, without api.ts importing React. */
let onUnauthorized: (() => void) | null = null
export function setUnauthorizedHandler(fn: (() => void) | null) {
  onUnauthorized = fn
}

// Empty by default: requests are same-origin, relative paths, resolved by
// the Vite dev proxy (vite.config.ts) or nginx (web/nginx.conf) in
// production, so no CORS headers are ever needed from the Go API server
// (which has none — see internal/api/ws/handler.go). Set VITE_API_BASE_URL
// to point at a differently-hosted API instead (e.g. local dev against a
// remote/staging api), at the cost of needing CORS support server-side.
const API_BASE = import.meta.env.VITE_API_BASE_URL ?? ''

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const token = tokenStorage.get()
  const headers = new Headers(init?.headers)
  headers.set('Content-Type', 'application/json')
  if (token) headers.set('Authorization', `Bearer ${token}`)

  const res = await fetch(`${API_BASE}${path}`, { ...init, headers })

  if (res.status === 401) {
    onUnauthorized?.()
  }

  if (!res.ok) {
    let message = res.statusText
    try {
      const body = await res.json()
      // grpc-gateway error responses are protojson-encoded google.rpc.Status:
      // {"code": <int>, "message": <string>, ...}. The plain /auth/* handlers
      // return {"error": <string>} via http.Error's plaintext body instead —
      // handled by the catch below when body isn't JSON.
      message = body.message ?? body.error ?? message
    } catch {
      // non-JSON error body (e.g. http.Error's plaintext) — keep statusText
    }
    throw new ApiError(res.status, message)
  }

  if (res.status === 204) return undefined as T
  return (await res.json()) as T
}

function buildQuery(params: object): string {
  const qs = new URLSearchParams()
  for (const [key, value] of Object.entries(params) as [string, unknown][]) {
    if (value === undefined || value === null || value === '') continue
    if (Array.isArray(value)) {
      for (const v of value) qs.append(key, String(v))
    } else {
      qs.set(key, String(value))
    }
  }
  const s = qs.toString()
  return s ? `?${s}` : ''
}

export const api = {
  auth: {
    login: (email: string, password: string) =>
      request<AuthResponse>('/auth/login', {
        method: 'POST',
        body: JSON.stringify({ email, password }),
      }),
    register: (email: string, name: string, password: string) =>
      request<AuthResponse>('/auth/register', {
        method: 'POST',
        body: JSON.stringify({ email, name, password }),
      }),
    whoAmI: () => request<WhoAmIResponse>('/v1/auth/me'),
  },
  sevs: {
    list: (params: ListSEVsParams = {}) =>
      request<ListSEVsResponse>(`/v1/sevs${buildQuery(params)}`),
    get: (id: string) => request<SEVResponse>(`/v1/sevs/${id}`),
  },
  reports: {
    dashboardMetrics: () =>
      request<DashboardMetricsResponse>('/v1/reports/dashboard'),
  },
}
