import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { LoginPage } from '@/pages/LoginPage'
import { renderWithProviders } from '@/test/utils'

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

describe('LoginPage', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.stubGlobal('fetch', vi.fn())
  })
  afterEach(() => vi.unstubAllGlobals())

  it('submits email and password to /auth/login', async () => {
    vi.mocked(fetch)
      .mockResolvedValueOnce(
        jsonResponse({ token: 'tok', user: { id: '1', email: 'a@b.com', name: 'Ada', org_role: 'viewer' } }),
      )
      .mockResolvedValueOnce(
        jsonResponse({ id: '1', email: 'a@b.com', name: 'Ada', avatar_url: '', org_role: 'viewer', oauth_provider: '' }),
      )

    renderWithProviders(<LoginPage />)
    const user = userEvent.setup()
    await user.type(screen.getByLabelText(/email/i), 'a@b.com')
    await user.type(screen.getByLabelText(/password/i), 'hunter2222')
    await user.click(screen.getByRole('button', { name: /sign in/i }))

    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(2))
    const [loginUrl, loginInit] = vi.mocked(fetch).mock.calls[0]
    expect(loginUrl).toBe('/auth/login')
    expect(JSON.parse(String(loginInit!.body))).toEqual({ email: 'a@b.com', password: 'hunter2222' })
  })

  it('shows the server error message on a failed login', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(jsonResponse({ error: 'invalid email or password' }, 401))

    renderWithProviders(<LoginPage />)
    const user = userEvent.setup()
    await user.type(screen.getByLabelText(/email/i), 'a@b.com')
    await user.type(screen.getByLabelText(/password/i), 'wrongpassword')
    await user.click(screen.getByRole('button', { name: /sign in/i }))

    expect(await screen.findByRole('alert')).toHaveTextContent('invalid email or password')
  })
})
