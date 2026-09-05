package sev_test

import (
	"testing"
	"time"

	"github.com/g8rswimmer/sevitout/internal/sev"
	"github.com/g8rswimmer/sevitout/internal/store"
)

var escalationNow = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func enabledConfig(level int16, minutes int32) map[int16]*store.EscalationConfig {
	return map[int16]*store.EscalationConfig{
		level: {SeverityLevel: level, ThresholdMinutes: minutes, Enabled: true},
	}
}

func startedAt(agoMinutes int) *time.Time {
	t := escalationNow.Add(-time.Duration(agoMinutes) * time.Minute)
	return &t
}

func TestEvaluateEscalations_FiresWhenOverThresholdAndNoIC(t *testing.T) {
	sevs := []*store.SEV{
		{ID: "sev-1", SeverityLevel: 1, StartedAt: startedAt(45)},
	}
	due := sev.EvaluateEscalations(sevs, map[string]bool{}, enabledConfig(1, 30), escalationNow)

	if len(due) != 1 || due[0].ID != "sev-1" {
		t.Fatalf("got %v, want [sev-1] to be due for escalation", due)
	}
}

func TestEvaluateEscalations_SkipsWhenICAssigned(t *testing.T) {
	sevs := []*store.SEV{
		{ID: "sev-1", SeverityLevel: 1, StartedAt: startedAt(45)},
	}
	due := sev.EvaluateEscalations(sevs, map[string]bool{"sev-1": true}, enabledConfig(1, 30), escalationNow)

	if len(due) != 0 {
		t.Fatalf("got %v, want none due — an IC is already assigned", due)
	}
}

func TestEvaluateEscalations_SkipsWhenUnderThreshold(t *testing.T) {
	sevs := []*store.SEV{
		{ID: "sev-1", SeverityLevel: 1, StartedAt: startedAt(10)},
	}
	due := sev.EvaluateEscalations(sevs, map[string]bool{}, enabledConfig(1, 30), escalationNow)

	if len(due) != 0 {
		t.Fatalf("got %v, want none due — only 10 minutes elapsed against a 30-minute threshold", due)
	}
}

func TestEvaluateEscalations_SkipsWhenAlreadyEscalated(t *testing.T) {
	prev := escalationNow.Add(-5 * time.Minute)
	sevs := []*store.SEV{
		{ID: "sev-1", SeverityLevel: 1, StartedAt: startedAt(45), EscalatedAt: &prev},
	}
	due := sev.EvaluateEscalations(sevs, map[string]bool{}, enabledConfig(1, 30), escalationNow)

	if len(due) != 0 {
		t.Fatalf("got %v, want none due — already escalated once", due)
	}
}

func TestEvaluateEscalations_SkipsWhenConfigDisabled(t *testing.T) {
	sevs := []*store.SEV{
		{ID: "sev-1", SeverityLevel: 1, StartedAt: startedAt(45)},
	}
	configs := map[int16]*store.EscalationConfig{
		1: {SeverityLevel: 1, ThresholdMinutes: 30, Enabled: false},
	}
	due := sev.EvaluateEscalations(sevs, map[string]bool{}, configs, escalationNow)

	if len(due) != 0 {
		t.Fatalf("got %v, want none due — severity level 1 escalation is disabled", due)
	}
}

func TestEvaluateEscalations_SkipsWhenNoConfigForSeverity(t *testing.T) {
	sevs := []*store.SEV{
		{ID: "sev-1", SeverityLevel: 2, StartedAt: startedAt(45)},
	}
	due := sev.EvaluateEscalations(sevs, map[string]bool{}, enabledConfig(1, 30), escalationNow)

	if len(due) != 0 {
		t.Fatalf("got %v, want none due — no escalation config for severity 2", due)
	}
}

func TestEvaluateEscalations_SkipsWhenNoStartedAt(t *testing.T) {
	sevs := []*store.SEV{
		{ID: "sev-1", SeverityLevel: 1, StartedAt: nil},
	}
	due := sev.EvaluateEscalations(sevs, map[string]bool{}, enabledConfig(1, 30), escalationNow)

	if len(due) != 0 {
		t.Fatalf("got %v, want none due — no StartedAt baseline to measure from", due)
	}
}
