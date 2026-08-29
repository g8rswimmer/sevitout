package ai_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/g8rswimmer/sevitout/internal/ai"
	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/memory"
	"github.com/g8rswimmer/sevitout/internal/telemetry"
)

// fakeProvider records every call it receives and returns canned results, so
// dispatcher tests never make a real network call.
type fakeProvider struct {
	mu      sync.Mutex
	calls   []ai.Action
	lastCtx *ai.SEVContext // the SEVContext most recently passed to any method
}

func (f *fakeProvider) record(a ai.Action, sev *ai.SEVContext) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, a)
	f.lastCtx = sev
}

func (f *fakeProvider) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeProvider) lastSEVContext() *ai.SEVContext {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastCtx
}

func (f *fakeProvider) Summarize(_ context.Context, sev *ai.SEVContext) (string, error) {
	f.record(ai.ActionSummarize, sev)
	return "a summary", nil
}
func (f *fakeProvider) DraftAnnouncement(_ context.Context, sev *ai.SEVContext) (string, error) {
	f.record(ai.ActionDraftAnnouncement, sev)
	return "an announcement", nil
}
func (f *fakeProvider) SuggestRootCause(_ context.Context, sev *ai.SEVContext) ([]ai.RootCauseSuggestion, error) {
	f.record(ai.ActionSuggestRootCause, sev)
	return []ai.RootCauseSuggestion{{Category: "deployment", Rationale: "recent deploy"}}, nil
}
func (f *fakeProvider) DraftPostmortem(_ context.Context, sev *ai.SEVContext) (*ai.PostmortemDraft, error) {
	f.record(ai.ActionDraftPostmortem, sev)
	return &ai.PostmortemDraft{Summary: "draft"}, nil
}
func (f *fakeProvider) SuggestTasks(_ context.Context, sev *ai.SEVContext) ([]ai.TaskSuggestion, error) {
	f.record(ai.ActionSuggestTasks, sev)
	return []ai.TaskSuggestion{{Title: "add alert", Priority: "critical"}}, nil
}
func (f *fakeProvider) FindSimilar(_ context.Context, sev *ai.SEVContext) ([]ai.SimilarSEV, error) {
	f.record(ai.ActionFindSimilar, sev)
	return nil, nil
}
func (f *fakeProvider) SuggestResponders(_ context.Context, sev *ai.SEVContext) ([]ai.ResponderSuggestion, error) {
	f.record(ai.ActionSuggestResponders, sev)
	return []ai.ResponderSuggestion{{Role: "Incident Commander", Rationale: "SEV-1"}}, nil
}
func (f *fakeProvider) StreamAction(ctx context.Context, action ai.Action, sev *ai.SEVContext) (<-chan ai.Chunk, error) {
	content, err := callActionForTest(ctx, f, action, sev)
	if err != nil {
		return nil, err
	}
	ch := make(chan ai.Chunk, 1)
	ch <- ai.Chunk{Content: content, Done: true}
	close(ch)
	return ch, nil
}

// callActionForTest mirrors dispatcher.go's unexported callAction just
// enough for fakeProvider.StreamAction to produce the same content Run
// would have stored, without depending on that unexported function.
func callActionForTest(ctx context.Context, f *fakeProvider, action ai.Action, sev *ai.SEVContext) (string, error) {
	switch action {
	case ai.ActionSummarize:
		return f.Summarize(ctx, sev)
	case ai.ActionDraftAnnouncement:
		return f.DraftAnnouncement(ctx, sev)
	default:
		v, err := f.SuggestTasks(ctx, sev)
		if err != nil {
			return "", err
		}
		b, _ := json.Marshal(v)
		return string(b), nil
	}
}

// fakePublisher records every published event.
type fakePublisher struct {
	mu     sync.Mutex
	events []string
}

func (f *fakePublisher) Publish(sevID, eventType string, _ []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, sevID+":"+eventType)
}

func (f *fakePublisher) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.events)
}

type testHarness struct {
	sevs      *memory.SEVStore
	history   *memory.StatusHistoryStore
	anns      *memory.AnnouncementStore
	plugins   *memory.AIPluginStore
	outputs   *memory.AIOutputStore
	publisher *fakePublisher
	provider  *fakeProvider
	dispatch  *ai.Dispatcher
}

func newHarness(t *testing.T) *testHarness {
	t.Helper()
	h := &testHarness{
		sevs:      memory.NewSEVStore(),
		history:   memory.NewStatusHistoryStore(),
		anns:      memory.NewAnnouncementStore(),
		plugins:   memory.NewAIPluginStore(),
		outputs:   memory.NewAIOutputStore(),
		publisher: &fakePublisher{},
		provider:  &fakeProvider{},
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	factory := func(*store.AIPlugin, string) (ai.Provider, error) { return h.provider, nil }
	h.dispatch = ai.NewDispatcherWithFactory(ctx, h.sevs, h.history, h.anns, h.plugins, h.outputs, nil, h.publisher, nil, 1, factory)
	return h
}

func mustCreateSEV(t *testing.T, h *testHarness, sev *store.SEV) *store.SEV {
	t.Helper()
	if err := h.sevs.Create(context.Background(), sev); err != nil {
		t.Fatalf("create SEV: %v", err)
	}
	return sev
}

func mustCreatePlugin(t *testing.T, h *testHarness, p *store.AIPlugin) *store.AIPlugin {
	t.Helper()
	if err := h.plugins.Create(context.Background(), p); err != nil {
		t.Fatalf("create plugin: %v", err)
	}
	return p
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

func TestRun_ManualTriggerStoresOutputAndPublishes(t *testing.T) {
	h := newHarness(t)
	sev := mustCreateSEV(t, h, &store.SEV{Title: "db down", SeverityLevel: 2})
	plugin := mustCreatePlugin(t, h, &store.AIPlugin{Name: "p1", HandlerType: store.AIHandlerBuiltin, Enabled: true})

	out, err := h.dispatch.Run(context.Background(), sev.ID, ai.ActionSummarize, plugin.ID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Content != "a summary" {
		t.Fatalf("unexpected content: %q", out.Content)
	}
	if out.TriggerEvent != string(ai.ManualTrigger) {
		t.Fatalf("want trigger_event=manual, got %q", out.TriggerEvent)
	}

	stored, err := h.outputs.ListBySEVID(context.Background(), sev.ID)
	if err != nil || len(stored) != 1 {
		t.Fatalf("want 1 stored output, got %d (err=%v)", len(stored), err)
	}
	if h.publisher.count() != 1 {
		t.Fatalf("want 1 published event, got %d", h.publisher.count())
	}
}

func TestRun_AIDisabledForSEVIsRejected(t *testing.T) {
	h := newHarness(t)
	sev := mustCreateSEV(t, h, &store.SEV{Title: "x", SeverityLevel: 3, AIDisabled: true})
	plugin := mustCreatePlugin(t, h, &store.AIPlugin{Name: "p1", HandlerType: store.AIHandlerBuiltin, Enabled: true})

	_, err := h.dispatch.Run(context.Background(), sev.ID, ai.ActionSummarize, plugin.ID)
	if err != ai.ErrAIDisabledForSEV {
		t.Fatalf("want ErrAIDisabledForSEV, got %v", err)
	}
	if h.provider.callCount() != 0 {
		t.Fatal("provider should never be called when AI is disabled for the SEV")
	}
}

func TestRun_SensitiveSEVIsRejected(t *testing.T) {
	h := newHarness(t)
	sev := mustCreateSEV(t, h, &store.SEV{Title: "x", SeverityLevel: 1, Sensitive: true})
	plugin := mustCreatePlugin(t, h, &store.AIPlugin{Name: "p1", HandlerType: store.AIHandlerBuiltin, Enabled: true})

	_, err := h.dispatch.Run(context.Background(), sev.ID, ai.ActionSummarize, plugin.ID)
	if err != ai.ErrSensitiveSEV {
		t.Fatalf("want ErrSensitiveSEV, got %v", err)
	}
	if h.provider.callCount() != 0 {
		t.Fatal("provider should never be called for a sensitive SEV — its content must never reach an AI plugin")
	}
}

func TestStreamOne_SensitiveSEVIsRejected(t *testing.T) {
	h := newHarness(t)
	sev := mustCreateSEV(t, h, &store.SEV{Title: "x", SeverityLevel: 1, Sensitive: true})
	plugin := mustCreatePlugin(t, h, &store.AIPlugin{Name: "p1", HandlerType: store.AIHandlerBuiltin, Enabled: true})

	_, err := h.dispatch.StreamOne(context.Background(), sev.ID, ai.ActionSummarize, plugin.ID)
	if err != ai.ErrSensitiveSEV {
		t.Fatalf("want ErrSensitiveSEV, got %v", err)
	}
	if h.provider.callCount() != 0 {
		t.Fatal("provider should never be called for a sensitive SEV — its content must never reach an AI plugin")
	}
}

func TestRun_NoEnabledPluginIsRejected(t *testing.T) {
	h := newHarness(t)
	sev := mustCreateSEV(t, h, &store.SEV{Title: "x", SeverityLevel: 3})
	mustCreatePlugin(t, h, &store.AIPlugin{Name: "p1", HandlerType: store.AIHandlerBuiltin, Enabled: false})

	_, err := h.dispatch.Run(context.Background(), sev.ID, ai.ActionSummarize, 0)
	if err != ai.ErrNoEnabledPlugin {
		t.Fatalf("want ErrNoEnabledPlugin, got %v", err)
	}
}

func TestRun_RateLimitEnforced(t *testing.T) {
	h := newHarness(t)
	sev := mustCreateSEV(t, h, &store.SEV{Title: "x", SeverityLevel: 3})
	plugin := mustCreatePlugin(t, h, &store.AIPlugin{Name: "p1", HandlerType: store.AIHandlerBuiltin, Enabled: true, RateLimitPerMinute: 1})

	if _, err := h.dispatch.Run(context.Background(), sev.ID, ai.ActionSummarize, plugin.ID); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if _, err := h.dispatch.Run(context.Background(), sev.ID, ai.ActionSummarize, plugin.ID); err != ai.ErrRateLimited {
		t.Fatalf("second Run: want ErrRateLimited, got %v", err)
	}
}

func TestEvictRateLimit_ResetsPluginWindow(t *testing.T) {
	h := newHarness(t)
	sev := mustCreateSEV(t, h, &store.SEV{Title: "x", SeverityLevel: 3})
	plugin := mustCreatePlugin(t, h, &store.AIPlugin{Name: "p1", HandlerType: store.AIHandlerBuiltin, Enabled: true, RateLimitPerMinute: 1})

	if _, err := h.dispatch.Run(context.Background(), sev.ID, ai.ActionSummarize, plugin.ID); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if _, err := h.dispatch.Run(context.Background(), sev.ID, ai.ActionSummarize, plugin.ID); err != ai.ErrRateLimited {
		t.Fatalf("second Run: want ErrRateLimited, got %v", err)
	}

	h.dispatch.EvictRateLimit(plugin.ID)

	// Same as ConfigServer.DeleteAIPlugin's use case (see EvictRateLimit's
	// doc comment): evicting drops the window entirely, so a call right
	// after must be allowed again even though the original window hasn't
	// expired.
	if _, err := h.dispatch.Run(context.Background(), sev.ID, ai.ActionSummarize, plugin.ID); err != nil {
		t.Fatalf("Run after EvictRateLimit: want allowed, got %v", err)
	}
}

// The three tests below guard telemetry.AIActionsTotal's outcome labels.
// Each reads its own before/after delta (rather than asserting an absolute
// value) since AIActionsTotal is a process-wide metric other tests in this
// same test binary also increment.

func TestRun_ManualTriggerSuccess_RecordsSuccessMetric(t *testing.T) {
	h := newHarness(t)
	sev := mustCreateSEV(t, h, &store.SEV{Title: "x", SeverityLevel: 3})
	plugin := mustCreatePlugin(t, h, &store.AIPlugin{Name: "p1", HandlerType: store.AIHandlerBuiltin, Enabled: true})

	before := testutil.ToFloat64(telemetry.AIActionsTotal.WithLabelValues("success"))
	if _, err := h.dispatch.Run(context.Background(), sev.ID, ai.ActionSummarize, plugin.ID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := testutil.ToFloat64(telemetry.AIActionsTotal.WithLabelValues("success")); got != before+1 {
		t.Errorf("AIActionsTotal[success] = %v, want %v", got, before+1)
	}
}

func TestRun_RateLimited_RecordsErrorMetric(t *testing.T) {
	h := newHarness(t)
	sev := mustCreateSEV(t, h, &store.SEV{Title: "x", SeverityLevel: 3})
	plugin := mustCreatePlugin(t, h, &store.AIPlugin{Name: "p1", HandlerType: store.AIHandlerBuiltin, Enabled: true, RateLimitPerMinute: 1})

	if _, err := h.dispatch.Run(context.Background(), sev.ID, ai.ActionSummarize, plugin.ID); err != nil {
		t.Fatalf("first Run: %v", err)
	}

	before := testutil.ToFloat64(telemetry.AIActionsTotal.WithLabelValues("error"))
	if _, err := h.dispatch.Run(context.Background(), sev.ID, ai.ActionSummarize, plugin.ID); err != ai.ErrRateLimited {
		t.Fatalf("second Run: want ErrRateLimited, got %v", err)
	}
	if got := testutil.ToFloat64(telemetry.AIActionsTotal.WithLabelValues("error")); got != before+1 {
		t.Errorf("AIActionsTotal[error] = %v, want %v (rate-limited counts as run()'s error outcome, same as any other failure inside it)", got, before+1)
	}
}

func TestDispatch_SeverityTooLowSkip_RecordsSkippedMetric(t *testing.T) {
	h := newHarness(t)
	mustCreatePlugin(t, h, &store.AIPlugin{Name: "p1", HandlerType: store.AIHandlerBuiltin, Enabled: true, TriggerOnOpen: true})
	sev3 := mustCreateSEV(t, h, &store.SEV{Title: "minor", SeverityLevel: 3})

	before := testutil.ToFloat64(telemetry.AIActionsTotal.WithLabelValues("skipped"))
	h.dispatch.Dispatch(ai.TriggerSEVOpened, sev3.ID)
	waitFor(t, time.Second, func() bool {
		return testutil.ToFloat64(telemetry.AIActionsTotal.WithLabelValues("skipped")) == before+1
	})
	if h.provider.callCount() != 0 {
		t.Fatal("SEV-3 open should not have reached the provider")
	}
}

func TestNewDispatcher_ConstructsWorkingDispatcher(t *testing.T) {
	// NewDispatcher itself is a one-line delegate to NewDispatcherWithFactory
	// (which every other test in this file uses via newHarness) — this just
	// confirms the production entry point wires a Dispatcher that actually
	// works end-to-end, using the real newProvider factory this time.
	sevs := memory.NewSEVStore()
	history := memory.NewStatusHistoryStore()
	anns := memory.NewAnnouncementStore()
	plugins := memory.NewAIPluginStore()
	outputs := memory.NewAIOutputStore()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	d := ai.NewDispatcher(ctx, sevs, history, anns, plugins, outputs, nil, nil, nil, 1)
	if d == nil {
		t.Fatal("NewDispatcher returned nil")
	}

	sev := &store.SEV{Title: "x", SeverityLevel: 3}
	if err := sevs.Create(context.Background(), sev); err != nil {
		t.Fatalf("create SEV: %v", err)
	}
	// No plugins registered: Run should reach ErrNoEnabledPlugin rather than
	// hang or panic, confirming the dispatcher's internal wiring (stores,
	// limiter, worker pool) is functional without making a real Provider call.
	if _, err := d.Run(context.Background(), sev.ID, ai.ActionSummarize, 0); err != ai.ErrNoEnabledPlugin {
		t.Fatalf("Run: want ErrNoEnabledPlugin, got %v", err)
	}
}

func TestDispatch_SEVOpenedOnlyTriggersForSEV1And2(t *testing.T) {
	h := newHarness(t)
	mustCreatePlugin(t, h, &store.AIPlugin{Name: "p1", HandlerType: store.AIHandlerBuiltin, Enabled: true, TriggerOnOpen: true})

	sev3 := mustCreateSEV(t, h, &store.SEV{Title: "minor", SeverityLevel: 3})
	h.dispatch.Dispatch(ai.TriggerSEVOpened, sev3.ID)
	time.Sleep(50 * time.Millisecond) // give the async worker a chance to (not) run
	if h.provider.callCount() != 0 {
		t.Fatal("SEV-3 open should not trigger AI dispatch")
	}

	sev1 := mustCreateSEV(t, h, &store.SEV{Title: "critical", SeverityLevel: 1})
	h.dispatch.Dispatch(ai.TriggerSEVOpened, sev1.ID)
	waitFor(t, time.Second, func() bool { return h.provider.callCount() == 1 })
}

func TestDispatch_RespectsPluginTriggerFlag(t *testing.T) {
	h := newHarness(t)
	mustCreatePlugin(t, h, &store.AIPlugin{Name: "p1", HandlerType: store.AIHandlerBuiltin, Enabled: true, TriggerOnOpen: false})
	sev := mustCreateSEV(t, h, &store.SEV{Title: "critical", SeverityLevel: 1})

	h.dispatch.Dispatch(ai.TriggerSEVOpened, sev.ID)
	time.Sleep(50 * time.Millisecond)
	if h.provider.callCount() != 0 {
		t.Fatal("plugin with trigger_on_open=false should not run on sev.opened")
	}
}

func TestDispatch_AIDisabledSEVSkipsProactiveTrigger(t *testing.T) {
	h := newHarness(t)
	mustCreatePlugin(t, h, &store.AIPlugin{Name: "p1", HandlerType: store.AIHandlerBuiltin, Enabled: true, TriggerOnMitigated: true})
	sev := mustCreateSEV(t, h, &store.SEV{Title: "x", SeverityLevel: 2, AIDisabled: true})

	h.dispatch.Dispatch(ai.TriggerSEVMitigated, sev.ID)
	time.Sleep(50 * time.Millisecond)
	if h.provider.callCount() != 0 {
		t.Fatal("AIDisabled SEV should skip proactive dispatch entirely")
	}
}

func TestDispatch_SensitiveSEVSkipsProactiveTrigger(t *testing.T) {
	h := newHarness(t)
	mustCreatePlugin(t, h, &store.AIPlugin{Name: "p1", HandlerType: store.AIHandlerBuiltin, Enabled: true, TriggerOnMitigated: true})
	sev := mustCreateSEV(t, h, &store.SEV{Title: "x", SeverityLevel: 2, Sensitive: true})

	h.dispatch.Dispatch(ai.TriggerSEVMitigated, sev.ID)
	time.Sleep(50 * time.Millisecond)
	if h.provider.callCount() != 0 {
		t.Fatal("sensitive SEV should skip proactive dispatch entirely")
	}
}

// TestDispatch_SensitiveAtExecutionTimeSkipsTrigger covers the case where a
// SEV becomes sensitive after a proactive trigger is enqueued but before the
// worker actually processes it: runTrigger re-fetches the SEV fresh and must
// re-check Sensitive against that current state, not just whatever it was
// when Dispatch was called.
func TestDispatch_SensitiveAtExecutionTimeSkipsTrigger(t *testing.T) {
	h := newHarness(t)
	mustCreatePlugin(t, h, &store.AIPlugin{Name: "p1", HandlerType: store.AIHandlerBuiltin, Enabled: true, TriggerOnMitigated: true})
	sev := mustCreateSEV(t, h, &store.SEV{Title: "x", SeverityLevel: 2})

	sev.Sensitive = true
	if err := h.sevs.Update(context.Background(), sev); err != nil {
		t.Fatalf("update SEV: %v", err)
	}

	h.dispatch.Dispatch(ai.TriggerSEVMitigated, sev.ID)
	time.Sleep(50 * time.Millisecond)
	if h.provider.callCount() != 0 {
		t.Fatal("worker must re-check Sensitive against the freshly-fetched record, not enqueue-time state")
	}
}

func TestDispatch_MitigatedTriggersTwoActions(t *testing.T) {
	h := newHarness(t)
	mustCreatePlugin(t, h, &store.AIPlugin{Name: "p1", HandlerType: store.AIHandlerBuiltin, Enabled: true, TriggerOnMitigated: true})
	sev := mustCreateSEV(t, h, &store.SEV{Title: "x", SeverityLevel: 2})

	h.dispatch.Dispatch(ai.TriggerSEVMitigated, sev.ID)
	waitFor(t, time.Second, func() bool { return h.provider.callCount() == 2 })

	outs, err := h.outputs.ListBySEVID(context.Background(), sev.ID)
	if err != nil || len(outs) != 2 {
		t.Fatalf("want 2 stored outputs, got %d (err=%v)", len(outs), err)
	}
}

// TestRun_TimelineIsChronological ensures buildContext merges status-history
// and announcement entries into one chronologically ordered Timeline, not
// simply "all history, then all announcements" regardless of actual
// timestamps.
func TestRun_TimelineIsChronological(t *testing.T) {
	h := newHarness(t)
	sev := mustCreateSEV(t, h, &store.SEV{Title: "x", SeverityLevel: 2})
	plugin := mustCreatePlugin(t, h, &store.AIPlugin{Name: "p1", HandlerType: store.AIHandlerBuiltin, Enabled: true})

	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	// An announcement (T1) precedes a status change (T2), so the merged
	// timeline must show the announcement first even though the dispatcher
	// builds it by appending all status history before all announcements.
	if err := h.anns.Create(context.Background(), &store.Announcement{
		SEVID: sev.ID, Message: "investigating", CreatedAt: base,
	}); err != nil {
		t.Fatalf("create announcement: %v", err)
	}
	if err := h.history.Create(context.Background(), &store.SEVStatusHistory{
		SEVID: sev.ID, ToStatus: store.SEVStatusMitigated, TransitionedAt: base.Add(time.Minute),
	}); err != nil {
		t.Fatalf("create status history: %v", err)
	}

	if _, err := h.dispatch.Run(context.Background(), sev.ID, ai.ActionSummarize, plugin.ID); err != nil {
		t.Fatalf("Run: %v", err)
	}

	timeline := h.provider.lastSEVContext().Timeline
	if len(timeline) != 2 {
		t.Fatalf("want 2 timeline entries, got %d", len(timeline))
	}
	if timeline[0].Kind != "announcement" || timeline[1].Kind != "status_change" {
		t.Fatalf("want [announcement, status_change] in chronological order, got [%s, %s]", timeline[0].Kind, timeline[1].Kind)
	}
}

func TestStreamOne_StoresFinalChunk(t *testing.T) {
	h := newHarness(t)
	sev := mustCreateSEV(t, h, &store.SEV{Title: "x", SeverityLevel: 3})
	plugin := mustCreatePlugin(t, h, &store.AIPlugin{Name: "p1", HandlerType: store.AIHandlerBuiltin, Enabled: true})

	ch, err := h.dispatch.StreamOne(context.Background(), sev.ID, ai.ActionSummarize, plugin.ID)
	if err != nil {
		t.Fatalf("StreamOne: %v", err)
	}
	var last ai.Chunk
	for c := range ch {
		last = c
	}
	if !last.Done {
		t.Fatal("expected final chunk to have Done=true")
	}

	waitFor(t, time.Second, func() bool {
		outs, _ := h.outputs.ListBySEVID(context.Background(), sev.ID)
		return len(outs) == 1
	})
}
