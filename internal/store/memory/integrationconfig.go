package memory

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/g8rswimmer/sevitout/internal/store"
)

// IntegrationConfigStore is an in-memory implementation of store.IntegrationConfigStore.
type IntegrationConfigStore struct {
	mu   sync.RWMutex
	data map[string]*store.IntegrationConfig // keyed by integration_type
	seq  atomic.Int64
}

func NewIntegrationConfigStore() *IntegrationConfigStore {
	return &IntegrationConfigStore{data: make(map[string]*store.IntegrationConfig)}
}

var _ store.IntegrationConfigStore = (*IntegrationConfigStore)(nil)

func (s *IntegrationConfigStore) Get(_ context.Context, integrationType string) (*store.IntegrationConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cfg, ok := s.data[integrationType]
	if !ok {
		return nil, store.ErrNotFound
	}
	cp := *cfg
	return &cp, nil
}

func (s *IntegrationConfigStore) Upsert(_ context.Context, cfg *store.IntegrationConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.data[cfg.IntegrationType]; ok {
		cfg.ID = existing.ID
	} else {
		cfg.ID = s.seq.Add(1)
	}
	cp := *cfg
	s.data[cfg.IntegrationType] = &cp
	return nil
}

func (s *IntegrationConfigStore) List(_ context.Context) ([]*store.IntegrationConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*store.IntegrationConfig, 0, len(s.data))
	for _, cfg := range s.data {
		cp := *cfg
		out = append(out, &cp)
	}
	return out, nil
}
