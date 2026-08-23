import { type ClassValue, clsx } from 'clsx'
import { twMerge } from 'tailwind-merge'

/** Merges Tailwind class lists, resolving conflicting utility classes — the
 * standard shadcn/ui helper, used by every component in components/ui. */
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}
