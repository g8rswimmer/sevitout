package memory

import (
	"context"
	"sync"

	"github.com/g8rswimmer/sevitout/internal/store"
)

// PostmortemStore is an in-memory implementation of store.PostmortemStore.
type PostmortemStore struct {
	mu   sync.RWMutex
	data map[string]*store.Postmortem // keyed by sev_id
	seq  int64
}

func NewPostmortemStore() *PostmortemStore {
	return &PostmortemStore{data: make(map[string]*store.Postmortem)}
}

var _ store.PostmortemStore = (*PostmortemStore)(nil)

func (s *PostmortemStore) Create(_ context.Context, pm *store.Postmortem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.data[pm.SEVID]; exists {
		return store.ErrConflict
	}
	s.seq++
	pm.ID = s.seq
	cp := *pm
	s.data[pm.SEVID] = &cp
	return nil
}

func (s *PostmortemStore) GetBySEVID(_ context.Context, sevID string) (*store.Postmortem, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	pm, ok := s.data[sevID]
	if !ok {
		return nil, store.ErrNotFound
	}
	cp := *pm
	return &cp, nil
}

func (s *PostmortemStore) Update(_ context.Context, pm *store.Postmortem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[pm.SEVID]; !ok {
		return store.ErrNotFound
	}
	cp := *pm
	s.data[pm.SEVID] = &cp
	return nil
}

func (s *PostmortemStore) CountByStatus(_ context.Context) (map[store.PostmortemStatus]int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	counts := make(map[store.PostmortemStatus]int)
	for _, pm := range s.data {
		counts[pm.Status]++
	}
	return counts, nil
}
