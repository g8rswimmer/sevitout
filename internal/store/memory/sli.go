package memory

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/g8rswimmer/sevitout/internal/store"
)

// SLIStore is an in-memory implementation of store.SLIStore.
type SLIStore struct {
	mu   sync.RWMutex
	data map[int64]*store.SLI
	seq  atomic.Int64
}

func NewSLIStore() *SLIStore {
	return &SLIStore{data: make(map[int64]*store.SLI)}
}

var _ store.SLIStore = (*SLIStore)(nil)

func (s *SLIStore) Create(_ context.Context, sli *store.SLI) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sli.ID = s.seq.Add(1)
	cp := *sli
	s.data[sli.ID] = &cp
	return nil
}

func (s *SLIStore) Delete(_ context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[id]; !ok {
		return store.ErrNotFound
	}
	delete(s.data, id)
	return nil
}

func (s *SLIStore) ListBySEVID(_ context.Context, sevID string) ([]*store.SLI, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*store.SLI
	for _, sli := range s.data {
		if sli.SEVID == sevID {
			cp := *sli
			out = append(out, &cp)
		}
	}
	return out, nil
}
