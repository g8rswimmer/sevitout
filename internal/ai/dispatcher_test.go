package ai_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/g8rswimmer/sevitout/internal/ai"
	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/memory"
)

// fakeProvider records every call it receives and returns canned results, so
// dispatcher tests never make a real network call.
type fakeProvider struct {
	mu    sync.Mutex
	calls []ai.Action
}

func (f *fakeProvider) record(a ai.Action) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, a)
}

func (f *fakeProvider) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeProvider) Summarize(_ context.Context, _ *ai.SEVContext) (string, error) {
	f.record(ai.ActionSummarize)
	return "a summary", nil
}
func (f *fakeProvider) DraftAnnouncement(_ context.Context, _ *ai.SEVContext) (string, error) {
	f.record(ai.ActionDraftAnnouncement)
	return "an announcement", nil
}
func (f *fakeProvider) SuggestRootCause(_ context.Context, _ *ai.SEVContext) ([]ai.RootCauseSuggestion, error) {
	f.record(ai.ActionSuggestRootCause)
	return []ai.RootCauseSuggestion{{Category: "deployment", Rationale: "recent deploy"}}, nil
}
func (f *fakeProvider) DraftPostmortem(_ context.Context, _ *ai.SEVContext) (*ai.PostmortemDraft, error) {
	f.record(ai.ActionDraftPostmortem)
	return &ai.PostmortemDraft{Summary: "draft"}, nil
}
func (f *fakeProvider) SuggestTasks(_ context.Context, _ *ai.SEVContext) ([]ai.TaskSuggestion, error) {
	f.record(ai.ActionSuggestTasks)
	return []ai.TaskSuggestion{{Title: "add alert", Priority: "critical"}}, nil
}
func (f *fakeProvider) FindSimilar(_ context.Context, _ *ai.SEVContext) ([]ai.SimilarSEV, error) {
	f.record(ai.ActionFindSimilar)
	return nil, nil
}
func (f *fakeProvider) SuggestResponders(_ context.Context, _ *ai.SEVContext) ([]ai.ResponderSuggestion, error) {
	f.record(ai.ActionSuggestResponders)
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
