import * as React from 'react'
import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '@/lib/utils'

const badgeVariants = cva(
  'inline-flex items-center rounded-md border px-2 py-0.5 text-xs font-medium',
  {
    variants: {
      variant: {
        default: 'border-transparent bg-primary text-primary-foreground',
        secondary: 'border-transparent bg-secondary text-secondary-foreground',
        outline: 'border-border text-foreground',
        destructive: 'border-transparent bg-destructive text-destructive-foreground',
        'sev-1': 'border-transparent bg-sev-1 text-white',
        'sev-2': 'border-transparent bg-sev-2 text-white',
        'sev-3': 'border-transparent bg-sev-3 text-black',
        'sev-4': 'border-transparent bg-sev-4 text-white',
      },
    },
    defaultVariants: { variant: 'default' },
  },
)

export interface BadgeProps
  extends React.HTMLAttributes<HTMLSpanElement>,
    VariantProps<typeof badgeVariants> {}

export function Badge({ className, variant, ...props }: BadgeProps) {
  return <span className={cn(badgeVariants({ variant }), className)} {...props} />
}

/** severity_level (1-4) → the matching `sev-N` badge variant. */
export function severityVariant(level: number): BadgeProps['variant'] {
  return `sev-${Math.min(4, Math.max(1, level))}` as BadgeProps['variant']
}
