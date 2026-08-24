import { describe, expect, it } from 'vitest'
import { buildPostmortemTemplate } from '@/lib/postmortemTemplate'
import type { SEVResponse } from '@/types/api'

function makeSev(overrides: Partial<SEVResponse> = {}): SEVResponse {
  return {
    id: 'SEV-2026-0001',
    title: 'Database outage',
    severity_level: 1,
    status: 'open',
    created_at: '2026-08-23T20:00:00Z',
    updated_at: '2026-08-23T20:00:00Z',
    ...overrides,
  }
}

describe('buildPostmortemTemplate', () => {
  it('fills every section from recorded SEV facts, with a lifecycle table of deltas', () => {
    const md = buildPostmortemTemplate(
      makeSev({
        description: 'Checkout was down for all customers.',
        started_at: '2026-08-23T20:00:00Z',
        detected_at: '2026-08-23T20:05:00Z',
        mitigated_at: '2026-08-23T20:20:00Z',
        resolved_at: '2026-08-23T20:30:00Z',
        root_cause_category: 'deployment',
        root_cause_description: 'A bad rollout introduced a nil pointer.',
        root_cause_reference_url: 'https://github.com/acme-corp/checkout-service/pull/123',
        business_impact: 'Lost an estimated $10k in checkout revenue.',
        affected_services: ['checkout', 'payments'],
        mitigation: 'Rolled back the deployment.',
      }),
    )

    expect(md).toContain('## Summary')
    expect(md).toContain('Checkout was down for all customers.')

    expect(md).toContain('## Lifecycle')
    expect(md).toContain('| Started |')
    expect(md).toContain('| Detected |')
    expect(md).toContain('5m 0s') // detected - started delta
    expect(md).toContain('15m 0s') // mitigated - detected delta
    expect(md).toContain('10m 0s') // resolved - mitigated delta

    expect(md).toContain('## Root Cause')
    expect(md).toContain('**Category:** Deployment')
    expect(md).toContain('A bad rollout introduced a nil pointer.')
    expect(md).toContain('**Reference:** https://github.com/acme-corp/checkout-service/pull/123')

    expect(md).toContain('## Business Impact')
    expect(md).toContain('Lost an estimated $10k in checkout revenue.')

    expect(md).toContain('## Services Affected')
    expect(md).toContain('- checkout')
    expect(md).toContain('- payments')

    expect(md).toContain('## Mitigation')
    expect(md).toContain('Rolled back the deployment.')
  })

  it('falls back to explanatory placeholders when nothing has been recorded yet', () => {
    const md = buildPostmortemTemplate(makeSev())

    expect(md).toContain('_No description provided._')
    expect(md).toContain('_No lifecycle timestamps recorded yet._')
    expect(md).toContain('_Not yet determined._')
    expect(md).toContain('_Not yet documented._')
    expect(md).toContain('_No services recorded._')
    expect(md).not.toContain('**Reference:**')
  })

  it("shows an unrecognized root cause category as its raw value ('Other' free text)", () => {
    const md = buildPostmortemTemplate(makeSev({ root_cause_category: 'flaky third-party API' }))
    expect(md).toContain('**Category:** flaky third-party API')
  })
})
