/** Formats a protojson int64 duration-in-seconds string (see types/api.ts's
 * header comment) as a short human string, e.g. "2h 14m", "45s", "3d 1h". */
export function formatDurationSeconds(seconds: string | number | undefined): string {
  const n = typeof seconds === 'string' ? Number(seconds) : seconds
  if (n === undefined || Number.isNaN(n) || n <= 0) return '—'

  const days = Math.floor(n / 86400)
  const hours = Math.floor((n % 86400) / 3600)
  const minutes = Math.floor((n % 3600) / 60)
  const secs = Math.floor(n % 60)

  if (days > 0) return `${days}d ${hours}h`
  if (hours > 0) return `${hours}h ${minutes}m`
  if (minutes > 0) return `${minutes}m ${secs}s`
  return `${secs}s`
}

export function formatDateTime(iso: string | undefined): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return '—'
  return d.toLocaleString(undefined, {
    dateStyle: 'medium',
    timeStyle: 'short',
  })
}

/** An ISO timestamp as a `yyyy-MM-ddThh:mm` value for
 * `<input type="datetime-local">`, in local time — the inverse of
 * `new Date(value).toISOString()`, which every datetime-local field in this
 * app uses to convert back on submit. */
export function toDateTimeLocalValue(iso: string | undefined): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}
