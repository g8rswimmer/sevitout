import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api, setUnauthorizedHandler, tokenStorage } from '@/lib/api'

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('api client', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.stubGlobal('fetch', vi.fn())
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    setUnauthorizedHandler(null)
  })

  it('sends no Authorization header when no token is stored', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(jsonResponse({ id: '1', email: 'a@b.com' }))
    await api.auth.whoAmI()
    const [, init] = vi.mocked(fetch).mock.calls[0]
    expect((init!.headers as Headers).has('Authorization')).toBe(false)
  })

  it('sends a Bearer Authorization header when a token is stored', async () => {
    tokenStorage.set('tok-123')
    vi.mocked(fetch).mockResolvedValueOnce(jsonResponse({ id: '1', email: 'a@b.com' }))
    await api.auth.whoAmI()
    const [, init] = vi.mocked(fetch).mock.calls[0]
    expect((init!.headers as Headers).get('Authorization')).toBe('Bearer tok-123')
  })

  it('parses a protojson-style error body ({message}) into ApiError', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(jsonResponse({ code: 3, message: 'invalid title' }, 400))
    await expect(api.sevs.get('sev-1')).rejects.toMatchObject({ status: 400, message: 'invalid title' })
  })

  it('parses a plaintext http.Error body ({error}) into ApiError', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(jsonResponse({ error: 'bad request' }, 400))
    await expect(api.auth.login('a@b.com', 'pw')).rejects.toMatchObject({ status: 400, message: 'bad request' })
  })

  it('falls back to statusText when the error body is not JSON', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(
      new Response('plain text failure', { status: 500, statusText: 'Server Error' }),
    )
    await expect(api.auth.whoAmI()).rejects.toMatchObject({ status: 500, message: 'Server Error' })
  })

  it('invokes the unauthorized handler on a 401, in addition to throwing', async () => {
    const onUnauthorized = vi.fn()
    setUnauthorizedHandler(onUnauthorized)
    vi.mocked(fetch).mockResolvedValueOnce(jsonResponse({ message: 'expired' }, 401))
    await expect(api.auth.whoAmI()).rejects.toThrow()
    expect(onUnauthorized).toHaveBeenCalledOnce()
  })

  it('serializes array filters as repeated query params', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(jsonResponse({ sevs: [], total: 0 }))
    await api.sevs.list({ severity_levels: [1, 2], statuses: ['open'], limit: 10 })
    const [url] = vi.mocked(fetch).mock.calls[0]
    const parsed = new URL(String(url), 'http://x')
    expect(parsed.searchParams.getAll('severity_levels')).toEqual(['1', '2'])
    expect(parsed.searchParams.getAll('statuses')).toEqual(['open'])
    expect(parsed.searchParams.get('limit')).toBe('10')
  })

  it('omits empty/undefined filter values from the query string', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(jsonResponse({ sevs: [], total: 0 }))
    await api.sevs.list({ search: '' })
    const [url] = vi.mocked(fetch).mock.calls[0]
    expect(String(url)).toBe('/v1/sevs')
  })
})

describe('tokenStorage', () => {
  beforeEach(() => localStorage.clear())

  it('round-trips a token through localStorage', () => {
    expect(tokenStorage.get()).toBeNull()
    tokenStorage.set('abc')
    expect(tokenStorage.get()).toBe('abc')
    tokenStorage.clear()
    expect(tokenStorage.get()).toBeNull()
  })
})
