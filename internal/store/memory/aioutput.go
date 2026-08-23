package memory

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/g8rswimmer/sevitout/internal/store"
)

// AIOutputStore is an in-memory implementation of store.AIOutputStore.
type AIOutputStore struct {
	mu   sync.RWMutex
	data map[string][]*store.AIOutput // sevID -> outputs, insertion order
	seq  atomic.Int64
}

func NewAIOutputStore() *AIOutputStore {
	return &AIOutputStore{data: make(map[string][]*store.AIOutput)}
}

var _ store.AIOutputStore = (*AIOutputStore)(nil)

func (s *AIOutputStore) Create(_ context.Context, output *store.AIOutput) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	output.ID = s.seq.Add(1)
	cp := *output
	s.data[output.SEVID] = append(s.data[output.SEVID], &cp)
	return nil
}

func (s *AIOutputStore) ListBySEVID(_ context.Context, sevID string) ([]*store.AIOutput, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := s.data[sevID]
	out := make([]*store.AIOutput, len(items))
	for i, o := range items {
		cp := *o
		out[i] = &cp
	}
	return out, nil
}
