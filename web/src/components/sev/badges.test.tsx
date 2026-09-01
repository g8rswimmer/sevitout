import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import { SLABadge } from '@/components/sev/badges'

describe('SLABadge', () => {
  it('renders nothing for "ok"', () => {
    const { container } = render(<SLABadge status="ok" />)
    expect(container).toBeEmptyDOMElement()
  })

  it('renders nothing for "not_applicable"', () => {
    const { container } = render(<SLABadge status="not_applicable" />)
    expect(container).toBeEmptyDOMElement()
  })

  it('renders nothing when status is undefined', () => {
    const { container } = render(<SLABadge />)
    expect(container).toBeEmptyDOMElement()
  })

  it('renders an "at risk" badge for "at_risk"', () => {
    render(<SLABadge status="at_risk" label="MTTD" />)
    expect(screen.getByText(/MTTD at risk/i)).toBeInTheDocument()
  })

  it('renders a "breached" badge for "breached"', () => {
    render(<SLABadge status="breached" label="MTTR" />)
    expect(screen.getByText(/MTTR breached/i)).toBeInTheDocument()
  })

  it('defaults the label to "SLA"', () => {
    render(<SLABadge status="breached" />)
    expect(screen.getByText(/SLA breached/i)).toBeInTheDocument()
  })
})
