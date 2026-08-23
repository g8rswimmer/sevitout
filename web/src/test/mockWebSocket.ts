/** A minimal fake WebSocket good enough for lib/ws.ts's usage (onmessage,
 * onclose, close()) — jsdom doesn't implement the real thing. Every
 * constructed instance is pushed onto `instances` so a test can grab the
 * most recent one and drive it (`.emitMessage(json)`, `.emitClose()`).
 *
 * Install it with, inline, in the consuming test file's own beforeEach:
 *
 *   beforeEach(() => {
 *     MockWebSocket.instances = []
 *     vi.stubGlobal('WebSocket', MockWebSocket)
 *   })
 *
 * Deliberately not wrapped in a shared `installMockWebSocket()` helper: with
 * this Vitest/esbuild combination, calling `vi.stubGlobal` through a
 * function defined in another module (or even a same-file wrapper function,
 * as opposed to inline statements directly in the hook body) throws
 * `TypeError: Class constructor MockWebSocket cannot be invoked without
 * 'new'` — reproducibly, for a test that never even constructs the class.
 * Calling `vi.stubGlobal` as a direct statement in the hook body sidesteps
 * it; a real bug report may be worth filing, but until then, don't
 * re-introduce the wrapper. */
export class MockWebSocket {
  static instances: MockWebSocket[] = []

  url: string
  onmessage: ((ev: { data: string }) => void) | null = null
  onclose: (() => void) | null = null
  closed = false

  constructor(url: string) {
    this.url = url
    MockWebSocket.instances.push(this)
  }

  emitMessage(data: unknown) {
    this.onmessage?.({ data: typeof data === 'string' ? data : JSON.stringify(data) })
  }

  emitClose() {
    this.onclose?.()
  }

  close() {
    this.closed = true
  }
}
