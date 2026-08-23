package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const httpProviderTimeout = 60 * time.Second

// HTTPProvider is the generic Provider for an externally hosted AI handler
// (docs/project-plan.md M12's "HTTP provider: generic implementation
// calling a configured external endpoint"). It POSTs one JSON request per
// action to endpoint and expects a JSON response shaped to match; the
// external handler owns whatever model/vendor it wants behind that contract.
type HTTPProvider struct {
	endpoint string
	apiKey   string // sent as a Bearer token; empty if the plugin has none configured
	http     *http.Client
}

// NewHTTPProvider returns an HTTPProvider that calls endpoint directly, for
// use both in production and in tests (point endpoint at an httptest.Server).
func NewHTTPProvider(endpoint, apiKey string) *HTTPProvider {
	return &HTTPProvider{endpoint: endpoint, apiKey: apiKey, http: &http.Client{Timeout: httpProviderTimeout}}
}

var _ Provider = (*HTTPProvider)(nil)

// httpActionRequest is the request body POSTed for every action. sev is the
// full SEVContext, so an external handler with richer needs than the
// built-in Anthropic prompts always has the same raw material available.
type httpActionRequest struct {
	Action Action      `json:"action"`
	SEV    *SEVContext `json:"sev"`
}

// httpActionResponse carries the field matching the requested action; every
// other field is left zero. This flat shape (rather than one endpoint per
// action) keeps a v1 external handler to a single route.
type httpActionResponse struct {
	Text       string                `json:"text,omitempty"` // Summarize, DraftAnnouncement
	RootCauses []RootCauseSuggestion `json:"root_causes,omitempty"`
	Postmortem *PostmortemDraft      `json:"postmortem,omitempty"`
	Tasks      []TaskSuggestion      `json:"tasks,omitempty"`
	Similar    []SimilarSEV          `json:"similar,omitempty"`
	Responders []ResponderSuggestion `json:"responders,omitempty"`
	Error      string                `json:"error,omitempty"`
}

func (p *HTTPProvider) call(ctx context.Context, action Action, sev *SEVContext) (*httpActionResponse, error) {
	body, err := json.Marshal(httpActionRequest{Action: action, SEV: sev})
	if err != nil {
		return nil, fmt.Errorf("http provider: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("http provider: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	resp, err := p.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http provider: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("http provider: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http provider: request failed: status %s: %s", resp.Status, raw)
	}
	var out httpActionResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("http provider: decode response: %w", err)
	}
	if out.Error != "" {
		return nil, fmt.Errorf("http provider: %s", out.Error)
	}
	return &out, nil
}

func (p *HTTPProvider) Summarize(ctx context.Context, sev *SEVContext) (string, error) {
	resp, err := p.call(ctx, ActionSummarize, sev)
	if err != nil {
		return "", err
	}
	return resp.Text, nil
}

func (p *HTTPProvider) DraftAnnouncement(ctx context.Context, sev *SEVContext) (string, error) {
	resp, err := p.call(ctx, ActionDraftAnnouncement, sev)
	if err != nil {
		return "", err
	}
	return resp.Text, nil
}

func (p *HTTPProvider) SuggestRootCause(ctx context.Context, sev *SEVContext) ([]RootCauseSuggestion, error) {
	resp, err := p.call(ctx, ActionSuggestRootCause, sev)
	if err != nil {
		return nil, err
	}
	return resp.RootCauses, nil
}

func (p *HTTPProvider) DraftPostmortem(ctx context.Context, sev *SEVContext) (*PostmortemDraft, error) {
	resp, err := p.call(ctx, ActionDraftPostmortem, sev)
	if err != nil {
		return nil, err
	}
	return resp.Postmortem, nil
}

func (p *HTTPProvider) SuggestTasks(ctx context.Context, sev *SEVContext) ([]TaskSuggestion, error) {
	resp, err := p.call(ctx, ActionSuggestTasks, sev)
	if err != nil {
		return nil, err
	}
	return resp.Tasks, nil
}

func (p *HTTPProvider) FindSimilar(ctx context.Context, sev *SEVContext) ([]SimilarSEV, error) {
	resp, err := p.call(ctx, ActionFindSimilar, sev)
	if err != nil {
		return nil, err
	}
	return resp.Similar, nil
}

func (p *HTTPProvider) SuggestResponders(ctx context.Context, sev *SEVContext) ([]ResponderSuggestion, error) {
	resp, err := p.call(ctx, ActionSuggestResponders, sev)
	if err != nil {
		return nil, err
	}
	return resp.Responders, nil
}

// StreamAction has no real streaming transport for an external HTTP
// handler in v1 (no SSE/websocket contract defined for it yet) — it runs
// the action to completion and re-emits it exactly like AnthropicProvider's
// StreamAction does. See that method's doc comment.
func (p *HTTPProvider) StreamAction(ctx context.Context, action Action, sev *SEVContext) (<-chan Chunk, error) {
	content, err := callAction(ctx, p, action, sev)
	if err != nil {
		return nil, err
	}
	return chunkText(ctx, content), nil
}
