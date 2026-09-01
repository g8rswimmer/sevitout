/** Plain-English definitions for each derived SEV timing metric —
 * docs/requirements.md §2.2 spells out the formula for MTTD/MTTM/MTTR/DTTM;
 * RTPC is a Phase 12 follow-up (an SLA target for the postmortem tail, not
 * just incident response). Shared between LifecyclePanel.tsx (per-SEV
 * values) and ServiceSLAEditor.tsx (per-service target column headers) so
 * both surfaces explain the acronyms identically. */
export const METRIC_DEFINITIONS = {
  MTTD: 'Mean Time to Detect — detected_at − started_at',
  MTTM: 'Mean Time to Mitigate — mitigated_at − started_at',
  MTTR: 'Mean Time to Resolve — resolved_at − started_at',
  DTTM: 'Detection to Mitigation — mitigated_at − detected_at',
  RTPC: 'Resolution to Postmortem Complete — postmortem_completed_at − resolved_at',
} as const
