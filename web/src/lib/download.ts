/** Triggers a browser download of `content` as a file named `filename` —
 * the standard Blob + object-URL + programmatic-anchor-click technique,
 * since there's no server-side file to link to. Deliberately generic (not
 * postmortem-specific): `docs/project-plan.md`'s M14e ("CSV export button on
 * SEV list") will want the exact same mechanism, and this is the first
 * download affordance in the app, so nothing else exists yet to reuse. */
export function downloadTextFile(filename: string, content: string, mimeType = 'text/plain'): void {
  const blob = new Blob([content], { type: mimeType })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
}
