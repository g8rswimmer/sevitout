import { describe, expect, it } from 'vitest'
import { formatDateTime, formatDurationSeconds } from '@/lib/format'

describe('formatDurationSeconds', () => {
  it('renders an em dash for missing/zero/negative durations', () => {
    expect(formatDurationSeconds(undefined)).toBe('—')
    expect(formatDurationSeconds('0')).toBe('—')
    expect(formatDurationSeconds(-5)).toBe('—')
  })

  it('formats seconds, minutes, hours, and days at the right granularity', () => {
    expect(formatDurationSeconds('45')).toBe('45s')
    expect(formatDurationSeconds('125')).toBe('2m 5s')
    expect(formatDurationSeconds('8100')).toBe('2h 15m')
    expect(formatDurationSeconds(String(3 * 86400 + 3600))).toBe('3d 1h')
  })
})

describe('formatDateTime', () => {
  it('renders an em dash for missing/invalid input', () => {
    expect(formatDateTime(undefined)).toBe('—')
    expect(formatDateTime('not-a-date')).toBe('—')
  })

  it('formats a valid ISO timestamp to a non-empty locale string', () => {
    const out = formatDateTime('2026-08-20T12:00:00Z')
    expect(out).not.toBe('—')
    expect(out.length).toBeGreaterThan(0)
  })
})
