package ai

import (
	"errors"
	"testing"

	"github.com/g8rswimmer/sevitout/internal/store"
)

func TestNewProvider_Builtin(t *testing.T) {
	model := "claude-opus-5"
	plugin := &store.AIPlugin{Name: "anthropic", HandlerType: store.AIHandlerBuiltin, Model: &model}

	p, err := newProvider(plugin, "test-key")
	if err != nil {
		t.Fatalf("newProvider: %v", err)
	}
	if _, ok := p.(*AnthropicProvider); !ok {
		t.Errorf("got %T, want *AnthropicProvider", p)
	}
}

func TestNewProvider_BuiltinDefaultsModel(t *testing.T) {
	plugin := &store.AIPlugin{Name: "anthropic", HandlerType: store.AIHandlerBuiltin}

	p, err := newProvider(plugin, "test-key")
	if err != nil {
		t.Fatalf("newProvider: %v", err)
	}
	if _, ok := p.(*AnthropicProvider); !ok {
		t.Errorf("got %T, want *AnthropicProvider", p)
	}
}

func TestNewProvider_HTTP(t *testing.T) {
	endpoint := "https://example.test/ai"
	plugin := &store.AIPlugin{Name: "external", HandlerType: store.AIHandlerHTTP, HTTPEndpoint: &endpoint}

	p, err := newProvider(plugin, "test-key")
	if err != nil {
		t.Fatalf("newProvider: %v", err)
	}
	if _, ok := p.(*HTTPProvider); !ok {
		t.Errorf("got %T, want *HTTPProvider", p)
	}
}

func TestNewProvider_HTTPMissingEndpoint(t *testing.T) {
	plugin := &store.AIPlugin{Name: "external", HandlerType: store.AIHandlerHTTP}

	if _, err := newProvider(plugin, "test-key"); err == nil {
		t.Fatal("want an error when http_endpoint is unconfigured")
	}
}

func TestNewProvider_UnsupportedHandlerType(t *testing.T) {
	plugin := &store.AIPlugin{Name: "mystery", HandlerType: store.AIHandlerType("carrier-pigeon")}

	_, err := newProvider(plugin, "test-key")
	if !errors.Is(err, ErrUnsupportedHandlerType) {
		t.Errorf("err = %v, want ErrUnsupportedHandlerType", err)
	}
}
