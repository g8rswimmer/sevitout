package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/telemetry"
)

// Publisher broadcasts a typed event to WebSocket clients subscribed to a
// SEV's room. Declared here (the consumer) rather than in package ws, per
// this repo's interface-ownership convention — ws.Hub satisfies it
// implicitly. Same shape as internal/api/grpc.Publisher; the two packages
// don't share one because internal/ai must not import internal/api/grpc.
type Publisher interface {
	Publish(sevID, eventType string, payload []byte)
}

// Decryptor decrypts an AI plugin's stored API key. Declared here (the
// consumer) so this package depends only on the one operation it needs;
// crypto.KeyEncryptor satisfies this implicitly, same as
// internal/api/grpc.Encryptor's Decrypt method.
type Decryptor interface {
	Decrypt(ciphertext []byte) ([]byte, error)
}

// taskQueueSize bounds how many proactive dispatches can be pending at once
// before Dispatch starts dropping them (logged, never blocking the caller).
const taskQueueSize = 256

// triggerActions maps a proactive lifecycle event to the Provider method(s)
// it runs (§11.1) and the AIPlugin trigger flag that must be set for a given
// plugin to participate. ManualTrigger (§11.2) bypasses this map entirely —
// the caller names the exact action.
var triggerActions = map[TriggerEvent]struct {
	actions []Action
	flag    func(*store.AIPlugin) bool
}{
	TriggerSEVOpened: {
		actions: []Action{ActionSuggestResponders},
		flag:    func(p *store.AIPlugin) bool { return p.TriggerOnOpen },
	},
	TriggerSEVMitigated: {
		actions: []Action{ActionSummarize, ActionSuggestRootCause},
		flag:    func(p *store.AIPlugin) bool { return p.TriggerOnMitigated },
	},
	TriggerSEVResolved: {
		actions: []Action{ActionDraftPostmortem},
		flag:    func(p *store.AIPlugin) bool { return p.TriggerOnResolved },
	},
	TriggerPostmortemInReview: {
		actions: []Action{ActionSuggestTasks},
		flag:    func(p *store.AIPlugin) bool { return p.TriggerOnPostmortemReview },
	},
}

// Dispatcher routes lifecycle events and on-demand requests to the
// configured AI plugin(s), stores results in ai_outputs, and broadcasts an
// ai.output WebSocket event for each. See docs/architecture.md §8.
type Dispatcher struct {
	sevs          store.SEVStore
	history       store.StatusHistoryStore
	announcements store.AnnouncementStore
	plugins       store.AIPluginStore
	outputs       store.AIOutputStore
	decryptor     Decryptor // nil disables all dispatch (no keys can be decrypted)
	publisher     Publisher // nil is a no-op, same convention as internal/api/grpc
	limiter       *RateLimiter
	newProvider   ProviderFactory
	log           *slog.Logger

	// bgCtx is used by the async worker goroutine instead of any per-request
	// context: a unary gRPC handler's context is canceled the moment the
	// handler returns, but proactive dispatch must keep running after that.
	// It lives exactly as long as the Dispatcher (canceled on process
	// shutdown via the ctx passed to NewDispatcher).
	bgCtx context.Context
	tasks chan dispatchTask
}

type dispatchTask struct {
	event TriggerEvent
	sevID string
}

// ProviderFactory builds the Provider a plugin's configuration selects,
// given its already-decrypted API key. Exported so tests can inject a fake
// factory (avoiding real network calls) via NewDispatcherWithFactory;
// production code gets the default (newProvider, routing on
// plugin.HandlerType) via NewDispatcher.
type ProviderFactory func(plugin *store.AIPlugin, apiKey string) (Provider, error)

// NewDispatcher returns a Dispatcher and starts its worker pool. ctx governs
// the pool's lifetime — cancel it to stop workers (e.g. on process
// shutdown). decryptor/publisher may be nil; see their doc comments.
func NewDispatcher(
	ctx context.Context,
	sevs store.SEVStore,
	history store.StatusHistoryStore,
	announcements store.AnnouncementStore,
	plugins store.AIPluginStore,
	outputs store.AIOutputStore,
	decryptor Decryptor,
	publisher Publisher,
	log *slog.Logger,
	workers int,
) *Dispatcher {
	return NewDispatcherWithFactory(ctx, sevs, history, announcements, plugins, outputs, decryptor, publisher, log, workers, newProvider)
}

// NewDispatcherWithFactory is NewDispatcher with the Provider construction
// step overridable — used by tests to substitute a fake Provider.
func NewDispatcherWithFactory(
	ctx context.Context,
	sevs store.SEVStore,
	history store.StatusHistoryStore,
	announcements store.AnnouncementStore,
	plugins store.AIPluginStore,
	outputs store.AIOutputStore,
	decryptor Decryptor,
	publisher Publisher,
	log *slog.Logger,
	workers int,
	factory ProviderFactory,
) *Dispatcher {
	if log == nil {
		log = slog.Default()
	}
	if workers <= 0 {
		workers = 2
	}
	if factory == nil {
		factory = newProvider
	}
	d := &Dispatcher{
		sevs:          sevs,
		history:       history,
		announcements: announcements,
		plugins:       plugins,
		outputs:       outputs,
		decryptor:     decryptor,
		publisher:     publisher,
		limiter:       NewRateLimiter(),
		newProvider:   factory,
		log:           log,
		bgCtx:         ctx,
		tasks:         make(chan dispatchTask, taskQueueSize),
	}
	for i := 0; i < workers; i++ {
		go d.worker()
	}
	return d
}

func (d *Dispatcher) worker() {
	for {
		select {
		case <-d.bgCtx.Done():
			return
		case t, ok := <-d.tasks:
			if !ok {
				return
			}
			d.runTrigger(d.bgCtx, t.event, t.sevID)
		}
	}
}

// Dispatch enqueues a proactive lifecycle trigger for async processing. It
// never blocks: a full queue drops the task (logged) rather than delaying
// the mutation handler that called it, matching the best-effort posture
// every other side effect in this codebase uses (e.g. publishProto).
func (d *Dispatcher) Dispatch(event TriggerEvent, sevID string) {
	select {
	case d.tasks <- dispatchTask{event: event, sevID: sevID}:
	default:
		d.log.Warn("ai dispatch queue full, dropping trigger", "event", event, "sev_id", sevID)
	}
}

// aiEligible reports whether sv may be sent to a configured AI plugin at
// all. Sensitive SEVs and SEVs with ai_disabled set are excluded from every
// dispatch path — proactive and on-demand alike — consistent with their
// other field-level visibility restrictions (§14, and M11's exclusion of
// sensitive SEVs from Slack incident channels). This is the single, shared
// gate for that rule: it's checked here (and re-checked by runTrigger against
// a freshly-fetched record, since a SEV's state can change between when a
// proactive trigger is enqueued and when a worker actually processes it) so
// no call site — proactive or on-demand — can independently drift out of
// sync with it.
func aiEligible(sv *store.SEV) error {
	if sv.Sensitive {
		return ErrSensitiveSEV
	}
	if sv.AIDisabled {
		return ErrAIDisabledForSEV
	}
	return nil
}

// runTrigger looks up the SEV fresh (so it reflects the state as of when the
// worker actually runs, not when it was enqueued), applies the trigger's
// gates, and runs the mapped action(s) for every plugin that opts in.
func (d *Dispatcher) runTrigger(ctx context.Context, event TriggerEvent, sevID string) {
	mapping, ok := triggerActions[event]
	if !ok {
		d.log.Error("ai dispatch: unknown trigger event", "event", event)
		return
	}

	sv, err := d.sevs.Get(ctx, sevID)
	if err != nil {
		d.log.ErrorContext(ctx, "ai dispatch: get SEV failed", "sev_id", sevID, "err", err)
		return
	}
	if err := aiEligible(sv); err != nil {
		d.log.DebugContext(ctx, "ai dispatch: skipped, SEV not eligible", "sev_id", sevID, "event", event, "reason", err)
		telemetry.AIActionsTotal.WithLabelValues("skipped").Inc()
		return
	}
	// SEV opened only proactively triggers AI for SEV-1/SEV-2 (§11.1); the
	// other three trigger events apply at any severity.
	if event == TriggerSEVOpened && sv.SeverityLevel > 2 {
		d.log.DebugContext(ctx, "ai dispatch: skipped, severity too low for sev.opened trigger",
			"sev_id", sevID, "severity_level", sv.SeverityLevel)
		telemetry.AIActionsTotal.WithLabelValues("skipped").Inc()
		return
	}

	plugins, err := d.plugins.List(ctx)
	if err != nil {
		d.log.ErrorContext(ctx, "ai dispatch: list plugins failed", "err", err)
		return
	}
	for _, plugin := range plugins {
		if !plugin.Enabled || !mapping.flag(plugin) {
			continue
		}
		for _, action := range mapping.actions {
			if _, err := d.run(ctx, sv, event, action, plugin); err != nil {
				d.log.ErrorContext(ctx, "ai dispatch: action failed",
					"sev_id", sevID, "plugin_id", plugin.ID, "action", action, "err", err)
			}
		}
	}
}

// Run executes one action against sevID synchronously, for a user-triggered
// request (§11.2, AIService.TriggerAction). pluginID selects a plugin; 0
// picks the first enabled plugin found.
func (d *Dispatcher) Run(ctx context.Context, sevID string, action Action, pluginID int64) (*store.AIOutput, error) {
	sv, err := d.sevs.Get(ctx, sevID)
	if err != nil {
		return nil, fmt.Errorf("get SEV: %w", err)
	}
	if err := aiEligible(sv); err != nil {
		return nil, err
	}
	plugin, err := d.resolvePlugin(ctx, pluginID)
	if err != nil {
		return nil, err
	}
	return d.run(ctx, sv, ManualTrigger, action, plugin)
}

// StreamOne is StreamAction's (§11.2) synchronous entry point: it resolves
// the same way Run does, then streams the provider's output chunks back to
// the caller. The final (Done) chunk is stored and published exactly like
// Run's result, once streaming completes — so a streamed action still shows
// up in ListOutputs and the ai.output WebSocket event.
func (d *Dispatcher) StreamOne(ctx context.Context, sevID string, action Action, pluginID int64) (<-chan Chunk, error) {
	sv, err := d.sevs.Get(ctx, sevID)
	if err != nil {
		return nil, fmt.Errorf("get SEV: %w", err)
	}
	if err := aiEligible(sv); err != nil {
		return nil, err
	}
	plugin, err := d.resolvePlugin(ctx, pluginID)
	if err != nil {
		return nil, err
	}
	if !d.limiter.Allow(plugin.ID, plugin.RateLimitPerMinute) {
		return nil, ErrRateLimited
	}
	provider, err := d.buildProvider(plugin)
	if err != nil {
		return nil, err
	}
	sevCtx, err := d.buildContext(ctx, sv)
	if err != nil {
		return nil, fmt.Errorf("build SEV context: %w", err)
	}

	upstream, err := provider.StreamAction(ctx, action, sevCtx)
	if err != nil {
		return nil, fmt.Errorf("provider stream action %s: %w", action, err)
	}

	out := make(chan Chunk)
	go func() {
		defer close(out)
		// upstream (built by chunkText, itself given ctx) is context-aware on
		// its sends, so exiting early on ctx.Done() below and abandoning the
		// range lets chunkText's own goroutine observe the same
		// cancellation on its next blocked send and exit too, instead of
		// leaking forever on an unbuffered channel nobody drains.
		for chunk := range upstream {
			select {
			case out <- chunk:
			case <-ctx.Done():
				return
			}
			if chunk.Done {
				o := &store.AIOutput{
					SEVID:        sv.ID,
					PluginID:     plugin.ID,
					TriggerEvent: string(ManualTrigger),
					Action:       string(action),
					Content:      chunk.Content,
					CreatedAt:    time.Now(),
				}
				if err := d.outputs.Create(ctx, o); err != nil {
					d.log.ErrorContext(ctx, "ai stream: store output failed", "sev_id", sv.ID, "err", err)
					continue
				}
				d.publish(sv.ID, o)
			}
		}
	}()
	return out, nil
}

// run is the shared core: build context, decrypt the key, construct the
// provider, rate-limit, call the action, persist, and publish. Both the
// async worker (runTrigger) and the synchronous Run path funnel through it.
// Named returns so the deferred outcome-metric recording below observes the
// actual result of whichever return statement fires, regardless of which
// internal step (rate limit, provider build, context build, the action
// itself, or the store write) produced it — one observation point per call,
// the same principle internal/api/grpc's logRPC uses for its own metrics.
func (d *Dispatcher) run(ctx context.Context, sv *store.SEV, event TriggerEvent, action Action, plugin *store.AIPlugin) (out *store.AIOutput, err error) {
	d.log.InfoContext(ctx, "ai dispatch: running action",
		"sev_id", sv.ID, "event", event, "action", action, "plugin_id", plugin.ID)
	defer func() {
		outcome := "success"
		if err != nil {
			outcome = "error"
		}
		telemetry.AIActionsTotal.WithLabelValues(outcome).Inc()
	}()

	if !d.limiter.Allow(plugin.ID, plugin.RateLimitPerMinute) {
		d.log.WarnContext(ctx, "ai dispatch: rate limited",
			"sev_id", sv.ID, "action", action, "plugin_id", plugin.ID)
		return nil, ErrRateLimited
	}

	provider, err := d.buildProvider(plugin)
	if err != nil {
		return nil, err
	}

	sevCtx, err := d.buildContext(ctx, sv)
	if err != nil {
		return nil, fmt.Errorf("build SEV context: %w", err)
	}

	content, err := callAction(ctx, provider, action, sevCtx)
	if err != nil {
		return nil, fmt.Errorf("provider action %s: %w", action, err)
	}

	out = &store.AIOutput{
		SEVID:        sv.ID,
		PluginID:     plugin.ID,
		TriggerEvent: string(event),
		Action:       string(action),
		Content:      content,
		CreatedAt:    time.Now(),
	}
	if err := d.outputs.Create(ctx, out); err != nil {
		return nil, fmt.Errorf("store output: %w", err)
	}

	d.log.InfoContext(ctx, "ai dispatch: action succeeded",
		"sev_id", sv.ID, "action", action, "plugin_id", plugin.ID, "output_id", out.ID, "content_len", len(content))
	d.publish(sv.ID, out)
	return out, nil
}

// EvictRateLimit drops pluginID's rate-limit window, if any. Callers should
// invoke this after permanently deleting a plugin (ConfigServer.DeleteAIPlugin)
// so a deleted-and-recreated plugin ID doesn't inherit a stale window, and so
// the limiter's map doesn't grow forever across delete/recreate cycles over a
// long-lived process.
func (d *Dispatcher) EvictRateLimit(pluginID int64) {
	d.limiter.Evict(pluginID)
}

func (d *Dispatcher) resolvePlugin(ctx context.Context, pluginID int64) (*store.AIPlugin, error) {
	if pluginID != 0 {
		p, err := d.plugins.Get(ctx, pluginID)
		if err != nil {
			return nil, fmt.Errorf("get plugin: %w", err)
		}
		if !p.Enabled {
			return nil, ErrPluginDisabled
		}
		return p, nil
	}
	plugins, err := d.plugins.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list plugins: %w", err)
	}
	for _, p := range plugins {
		if p.Enabled {
			return p, nil
		}
	}
	return nil, ErrNoEnabledPlugin
}

func (d *Dispatcher) buildProvider(plugin *store.AIPlugin) (Provider, error) {
	var apiKey string
	if len(plugin.EncryptedAPIKey) > 0 {
		if d.decryptor == nil {
			return nil, ErrEncryptionNotConfigured
		}
		plain, err := d.decryptor.Decrypt(plugin.EncryptedAPIKey)
		if err != nil {
			return nil, fmt.Errorf("decrypt api key: %w", err)
		}
		apiKey = string(plain)
	}
	return d.newProvider(plugin, apiKey)
}

func (d *Dispatcher) publish(sevID string, out *store.AIOutput) {
	if d.publisher == nil {
		return
	}
	b, err := json.Marshal(out)
	if err != nil {
		return
	}
	d.publisher.Publish(sevID, "ai.output", b)
}

// callAction dispatches to the right Provider method and normalizes its
// result to the string persisted in ai_outputs.content: plain text for
// narrative actions, JSON for structured, list-shaped ones.
func callAction(ctx context.Context, p Provider, action Action, sev *SEVContext) (string, error) {
	switch action {
	case ActionSummarize:
		return p.Summarize(ctx, sev)
	case ActionDraftAnnouncement:
		return p.DraftAnnouncement(ctx, sev)
	case ActionSuggestRootCause:
		v, err := p.SuggestRootCause(ctx, sev)
		return marshalOrErr(v, err)
	case ActionDraftPostmortem:
		v, err := p.DraftPostmortem(ctx, sev)
		return marshalOrErr(v, err)
	case ActionSuggestTasks:
		v, err := p.SuggestTasks(ctx, sev)
		return marshalOrErr(v, err)
	case ActionFindSimilar:
		v, err := p.FindSimilar(ctx, sev)
		return marshalOrErr(v, err)
	case ActionSuggestResponders:
		v, err := p.SuggestResponders(ctx, sev)
		return marshalOrErr(v, err)
	default:
		return "", fmt.Errorf("%w: %s", ErrUnknownAction, action)
	}
}

func marshalOrErr(v any, err error) (string, error) {
	if err != nil {
		return "", err
	}
	b, merr := json.Marshal(v)
	if merr != nil {
		return "", merr
	}
	return string(b), nil
}

// buildContext assembles a SEVContext from the store layer: the SEV's own
// fields, its status history and announcements merged into a timeline, and
// up to 5 other SEVs sharing an affected service as similarity candidates.
func (d *Dispatcher) buildContext(ctx context.Context, sv *store.SEV) (*SEVContext, error) {
	sc := &SEVContext{
		ID:               sv.ID,
		Title:            sv.Title,
		Description:      sv.Description,
		SeverityLevel:    sv.SeverityLevel,
		Status:           string(sv.Status),
		AffectedServices: sv.AffectedServices,
		StartedAt:        sv.StartedAt,
		DetectedAt:       sv.DetectedAt,
		MitigatedAt:      sv.MitigatedAt,
		ResolvedAt:       sv.ResolvedAt,
	}
	if sv.RootCauseCategory != nil {
		sc.RootCauseCategory = *sv.RootCauseCategory
	}
	if sv.RootCauseDescription != nil {
		sc.RootCauseDescription = *sv.RootCauseDescription
	}
	if sv.Mitigation != nil {
		sc.Mitigation = *sv.Mitigation
	}
	if sv.Prevention != nil {
		sc.Prevention = *sv.Prevention
	}
	if sv.BusinessImpact != nil {
		sc.BusinessImpact = *sv.BusinessImpact
	}
	if sv.DetectionMethod != nil {
		sc.DetectionMethod = *sv.DetectionMethod
	}

	if d.history != nil {
		hist, err := d.history.ListBySEVID(ctx, sv.ID)
		if err != nil {
			return nil, fmt.Errorf("list status history: %w", err)
		}
		for _, h := range hist {
			from := "(none)"
			if h.FromStatus != nil {
				from = string(*h.FromStatus)
			}
			sc.Timeline = append(sc.Timeline, TimelineEntry{
				At:      h.TransitionedAt,
				Kind:    "status_change",
				Summary: fmt.Sprintf("%s -> %s", from, h.ToStatus),
			})
		}
	}
	if d.announcements != nil {
		anns, err := d.announcements.ListBySEVID(ctx, sv.ID)
		if err != nil {
			return nil, fmt.Errorf("list announcements: %w", err)
		}
		for _, a := range anns {
			sc.Timeline = append(sc.Timeline, TimelineEntry{
				At:      a.CreatedAt,
				Kind:    "announcement",
				Summary: a.Message,
			})
		}
	}

	// Status-history and announcement entries are appended in two separate
	// loops above, so the merged slice isn't necessarily chronological —
	// sort it into one narrative before it reaches the AI provider.
	sort.Slice(sc.Timeline, func(i, j int) bool { return sc.Timeline[i].At.Before(sc.Timeline[j].At) })

	if len(sv.AffectedServices) > 0 {
		candidates, err := d.sevs.List(ctx, store.SEVFilter{
			ServiceIDs:       sv.AffectedServices,
			ExcludeSensitive: true,
			Limit:            6,
		})
		if err != nil {
			return nil, fmt.Errorf("list similar SEVs: %w", err)
		}
		for _, c := range candidates {
			if c.ID == sv.ID || len(sc.Similar) >= 5 {
				continue
			}
			category := ""
			if c.RootCauseCategory != nil {
				category = *c.RootCauseCategory
			}
			sc.Similar = append(sc.Similar, SimilarSEVSummary{ID: c.ID, Title: c.Title, RootCauseCategory: category})
		}
	}

	return sc, nil
}
