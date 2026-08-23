package ai

import (
	"fmt"
	"strings"
)

// System prompts, one per Action. Every prompt reiterates that output is
// AI-generated and non-authoritative (§11) and, for structured actions,
// pins down the exact JSON shape expected so completeJSON can parse it
// straight into the matching Go type.
const (
	summarizeSystemPrompt = "You are an incident response assistant. Write a concise, " +
		"blameless narrative summary (3-5 sentences) of the incident described below, " +
		"suitable for someone catching up on the situation. Respond with plain text only, " +
		"no markdown headers."

	draftAnnouncementSystemPrompt = "You are an incident response assistant drafting a " +
		"stakeholder-facing status update for the incident described below. Write one " +
		"short paragraph, factual and non-alarmist, appropriate to post to an internal " +
		"or external announcement channel. Respond with plain text only."

	suggestRootCauseSystemPrompt = `You are an incident response assistant. Based on the incident described below, suggest 1-3 likely root cause categories (e.g. deployment, configuration, hardware, dependency) with a one-sentence rationale each. Respond with only a JSON array, no markdown fence, matching this shape:
[{"category": "...", "rationale": "..."}]`

	draftPostmortemSystemPrompt = `You are an incident response assistant drafting a blameless postmortem skeleton from the incident described below. Fill in what the available data supports and leave a clear placeholder like "TBD" for anything not yet known. Respond with only a JSON object, no markdown fence, matching this shape:
{"summary": "...", "customer_impact": "...", "timeline": "...", "root_cause": "...", "contributing_factors": "...", "lessons_learned": "...", "action_items": "..."}`

	suggestTasksSystemPrompt = `You are an incident response assistant. Based on the incident described below (especially its root cause and timeline), suggest 1-5 concrete follow-up tasks to prevent recurrence. relationship_type must be one of: action-item, contributing-factor, follow-up, dependency. priority must be one of: critical, non-critical. Respond with only a JSON array, no markdown fence, matching this shape:
[{"title": "...", "description": "...", "relationship_type": "...", "priority": "..."}]`

	findSimilarSystemPrompt = `You are an incident response assistant. From the "similar SEV candidates" list below, pick the ones that plausibly share a root cause or failure pattern with the current incident, and explain why. If none are plausibly similar, respond with an empty array. Respond with only a JSON array, no markdown fence, matching this shape:
[{"sev_id": "...", "title": "...", "reason": "..."}]`

	suggestRespondersSystemPrompt = `You are an incident response assistant. A high-severity incident was just opened. Based on its affected services and description, suggest which roles should be staffed first (e.g. Incident Commander, specific service owners as Responders) and why. Respond with only a JSON array, no markdown fence, matching this shape:
[{"role": "...", "rationale": "..."}]`
)

// sevPrompt renders sev into the plain-text incident description every
// prompt above refers to as "the incident described below".
func sevPrompt(sev *SEVContext) string {
	var b strings.Builder
	fmt.Fprintf(&b, "SEV %s: %s\n", sev.ID, sev.Title)
	fmt.Fprintf(&b, "Severity: SEV-%d\n", sev.SeverityLevel)
	fmt.Fprintf(&b, "Status: %s\n", sev.Status)
	if sev.Description != "" {
		fmt.Fprintf(&b, "Description: %s\n", sev.Description)
	}
	if len(sev.AffectedServices) > 0 {
		fmt.Fprintf(&b, "Affected services: %s\n", strings.Join(sev.AffectedServices, ", "))
	}
	if sev.DetectionMethod != "" {
		fmt.Fprintf(&b, "Detection method: %s\n", sev.DetectionMethod)
	}
	if sev.RootCauseCategory != "" || sev.RootCauseDescription != "" {
		fmt.Fprintf(&b, "Root cause: %s — %s\n", sev.RootCauseCategory, sev.RootCauseDescription)
	}
	if sev.Mitigation != "" {
		fmt.Fprintf(&b, "Mitigation: %s\n", sev.Mitigation)
	}
	if sev.Prevention != "" {
		fmt.Fprintf(&b, "Prevention notes: %s\n", sev.Prevention)
	}
	if sev.BusinessImpact != "" {
		fmt.Fprintf(&b, "Business impact: %s\n", sev.BusinessImpact)
	}
	if len(sev.Timeline) > 0 {
		b.WriteString("Timeline:\n")
		for _, e := range sev.Timeline {
			fmt.Fprintf(&b, "- [%s] (%s) %s\n", e.At.Format("2006-01-02T15:04:05Z"), e.Kind, e.Summary)
		}
	}
	if len(sev.Similar) > 0 {
		b.WriteString("Similar SEV candidates (from the same affected service(s)):\n")
		for _, s := range sev.Similar {
			fmt.Fprintf(&b, "- %s: %s (root cause category: %s)\n", s.ID, s.Title, s.RootCauseCategory)
		}
	}
	return b.String()
}
