package ai

import (
	"fmt"

	"github.com/g8rswimmer/sevitout/internal/store"
)

// newProvider builds the Provider a plugin's configuration selects. It is
// the default value of Dispatcher.newProvider; tests override that field
// with a fake to avoid making real HTTP calls.
func newProvider(plugin *store.AIPlugin, apiKey string) (Provider, error) {
	switch plugin.HandlerType {
	case store.AIHandlerBuiltin:
		model := "claude-sonnet-5"
		if plugin.Model != nil && *plugin.Model != "" {
			model = *plugin.Model
		}
		return NewAnthropicProvider(apiKey, model), nil
	case store.AIHandlerHTTP:
		if plugin.HTTPEndpoint == nil || *plugin.HTTPEndpoint == "" {
			return nil, fmt.Errorf("ai: http plugin %q has no http_endpoint configured", plugin.Name)
		}
		return NewHTTPProvider(*plugin.HTTPEndpoint, apiKey), nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedHandlerType, plugin.HandlerType)
	}
}
