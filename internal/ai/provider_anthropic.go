package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultAnthropicBaseURL = "https://api.anthropic.com"
	anthropicAPIVersion     = "2023-06-01"
	anthropicTimeout        = 60 * time.Second
	defaultMaxTokens        = 2048
)

// AnthropicProvider is the built-in Provider backed by Anthropic's Messages
// API (docs/architecture.md §8, docs/project-plan.md M12).
type AnthropicProvider struct {
	apiKey  string
	model   string
	baseURL string
	http    *http.Client
}

// NewAnthropicProvider returns an AnthropicProvider that calls the real
// Anthropic API.
func NewAnthropicProvider(apiKey, model string) *AnthropicProvider {
	return NewAnthropicProviderWithBaseURL(apiKey, model, defaultAnthropicBaseURL)
}

// NewAnthropicProviderWithBaseURL returns an AnthropicProvider against a
// custom base URL, for tests with httptest.Server.
func NewAnthropicProviderWithBaseURL(apiKey, model, baseURL string) *AnthropicProvider {
	return &AnthropicProvider{
		apiKey:  apiKey,
		model:   model,
		baseURL: baseURL,
		http:    &http.Client{Timeout: anthropicTimeout},
	}
}

var _ Provider = (*AnthropicProvider)(nil)

func (p *AnthropicProvider) Summarize(ctx context.Context, sev *SEVContext) (string, error) {
	return p.complete(ctx, summarizeSystemPrompt, sevPrompt(sev))
}

func (p *AnthropicProvider) DraftAnnouncement(ctx context.Context, sev *SEVContext) (string, error) {
	return p.complete(ctx, draftAnnouncementSystemPrompt, sevPrompt(sev))
}

func (p *AnthropicProvider) SuggestRootCause(ctx context.Context, sev *SEVContext) ([]RootCauseSuggestion, error) {
	var out []RootCauseSuggestion
	if err := p.completeJSON(ctx, suggestRootCauseSystemPrompt, sevPrompt(sev), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (p *AnthropicProvider) DraftPostmortem(ctx context.Context, sev *SEVContext) (*PostmortemDraft, error) {
	var out PostmortemDraft
	if err := p.completeJSON(ctx, draftPostmortemSystemPrompt, sevPrompt(sev), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (p *AnthropicProvider) SuggestTasks(ctx context.Context, sev *SEVContext) ([]TaskSuggestion, error) {
	var out []TaskSuggestion
	if err := p.completeJSON(ctx, suggestTasksSystemPrompt, sevPrompt(sev), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (p *AnthropicProvider) FindSimilar(ctx context.Context, sev *SEVContext) ([]SimilarSEV, error) {
	var out []SimilarSEV
	if err := p.completeJSON(ctx, findSimilarSystemPrompt, sevPrompt(sev), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (p *AnthropicProvider) SuggestResponders(ctx context.Context, sev *SEVContext) ([]ResponderSuggestion, error) {
	var out []ResponderSuggestion
	if err := p.completeJSON(ctx, suggestRespondersSystemPrompt, sevPrompt(sev), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// StreamAction runs action to completion and then re-emits its result as a
// handful of word-chunked Chunks. Anthropic's Messages API does support
// real token-level SSE streaming, but wiring that through end-to-end
// (provider -> Dispatcher -> gRPC server-stream) is deferred past v1 — see
// demo/M12-ai-plugin.md's Known limitations. Every action is still
// available via StreamAction; the streaming is just coarser than
// token-by-token.
func (p *AnthropicProvider) StreamAction(ctx context.Context, action Action, sev *SEVContext) (<-chan Chunk, error) {
	content, err := callAction(ctx, p, action, sev)
	if err != nil {
		return nil, err
	}
	return chunkText(content), nil
}

// ---------- Messages API plumbing ----------

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// complete sends one request and returns the concatenated text content.
func (p *AnthropicProvider) complete(ctx context.Context, system, userPrompt string) (string, error) {
	body, err := json.Marshal(anthropicRequest{
		Model:     p.model,
		MaxTokens: defaultMaxTokens,
		System:    system,
		Messages:  []anthropicMessage{{Role: "user", Content: userPrompt}},
	})
	if err != nil {
		return "", fmt.Errorf("anthropic: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("anthropic: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", anthropicAPIVersion)

	resp, err := p.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("anthropic: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("anthropic: read response: %w", err)
	}

	var parsed anthropicResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("anthropic: decode response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		if parsed.Error != nil {
			return "", fmt.Errorf("anthropic: %s (status %s)", parsed.Error.Message, resp.Status)
		}
		return "", fmt.Errorf("anthropic: request failed: status %s", resp.Status)
	}

	var sb strings.Builder
	for _, block := range parsed.Content {
		sb.WriteString(block.Text)
	}
	return sb.String(), nil
}

// completeJSON is complete plus instructing the model to answer with only
// JSON matching out's shape, then decoding the response into out. Anthropic
// sometimes wraps JSON in a ```json fence despite instructions not to —
// stripJSONFence handles that.
func (p *AnthropicProvider) completeJSON(ctx context.Context, system, userPrompt string, out any) error {
	text, err := p.complete(ctx, system, userPrompt)
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(stripJSONFence(text)), out); err != nil {
		return fmt.Errorf("anthropic: parse JSON response: %w (raw: %s)", err, text)
	}
	return nil
}

func stripJSONFence(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

// chunkText splits text into a handful of word-grouped Chunks for
// StreamAction, terminated by a final empty Done chunk carrying the full
// text (so a caller that only reads the last chunk still gets the complete,
// storable result).
func chunkText(text string) <-chan Chunk {
	ch := make(chan Chunk)
	go func() {
		defer close(ch)
		const wordsPerChunk = 12
		words := strings.Fields(text)
		for i := 0; i < len(words); i += wordsPerChunk {
			end := i + wordsPerChunk
			if end > len(words) {
				end = len(words)
			}
			ch <- Chunk{Content: strings.Join(words[i:end], " ") + " "}
		}
		ch <- Chunk{Content: text, Done: true}
	}()
	return ch
}
