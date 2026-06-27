package memory

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/g8rswimmer/sevitout/internal/store"
)

// AIPluginStore is an in-memory implementation of store.AIPluginStore.
type AIPluginStore struct {
	mu   sync.RWMutex
	data map[int64]*store.AIPlugin
	seq  atomic.Int64
}

func NewAIPluginStore() *AIPluginStore {
	return &AIPluginStore{data: make(map[int64]*store.AIPlugin)}
}

var _ store.AIPluginStore = (*AIPluginStore)(nil)

func (s *AIPluginStore) Create(_ context.Context, plugin *store.AIPlugin) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.data {
		if existing.Name == plugin.Name {
			return store.ErrConflict
		}
	}
	plugin.ID = s.seq.Add(1)
	cp := *plugin
	s.data[plugin.ID] = &cp
	return nil
}

func (s *AIPluginStore) Get(_ context.Context, id int64) (*store.AIPlugin, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.data[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	cp := *p
	return &cp, nil
}

func (s *AIPluginStore) Update(_ context.Context, plugin *store.AIPlugin) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[plugin.ID]; !ok {
		return store.ErrNotFound
	}
	cp := *plugin
	s.data[plugin.ID] = &cp
	return nil
}

func (s *AIPluginStore) Delete(_ context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[id]; !ok {
		return store.ErrNotFound
	}
	delete(s.data, id)
	return nil
}

func (s *AIPluginStore) List(_ context.Context) ([]*store.AIPlugin, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*store.AIPlugin, 0, len(s.data))
	for _, p := range s.data {
		cp := *p
		out = append(out, &cp)
	}
	return out, nil
}
