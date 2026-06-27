package memory

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/g8rswimmer/sevitout/internal/store"
)

// OnCallStore is an in-memory implementation of store.OnCallStore.
type OnCallStore struct {
	mu   sync.RWMutex
	data map[int64]*store.OnCallRotation
	seq  atomic.Int64
}

func NewOnCallStore() *OnCallStore {
	return &OnCallStore{data: make(map[int64]*store.OnCallRotation)}
}

var _ store.OnCallStore = (*OnCallStore)(nil)

func (s *OnCallStore) Create(_ context.Context, r *store.OnCallRotation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r.ID = s.seq.Add(1)
	cp := *r
	s.data[r.ID] = &cp
	return nil
}

func (s *OnCallStore) Get(_ context.Context, id int64) (*store.OnCallRotation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.data[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	cp := *r
	return &cp, nil
}

func (s *OnCallStore) Update(_ context.Context, r *store.OnCallRotation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[r.ID]; !ok {
		return store.ErrNotFound
	}
	cp := *r
	s.data[r.ID] = &cp
	return nil
}

func (s *OnCallStore) Delete(_ context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[id]; !ok {
		return store.ErrNotFound
	}
	delete(s.data, id)
	return nil
}

func (s *OnCallStore) List(_ context.Context) ([]*store.OnCallRotation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*store.OnCallRotation, 0, len(s.data))
	for _, r := range s.data {
		cp := *r
		out = append(out, &cp)
	}
	return out, nil
}

// GetCurrentOnCall returns the active rotation for the given service.
// It prefers manual overrides whose time window contains now, then falls back to
// the first normal rotation (no full override set) linked to the service.
// Expired overrides are excluded entirely, matching the Postgres SQL behaviour.
func (s *OnCallStore) GetCurrentOnCall(_ context.Context, serviceID string) (*store.OnCallRotation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now()
	var fallback *store.OnCallRotation
	for _, r := range s.data {
		if r.ServiceID == nil || *r.ServiceID != serviceID {
			continue
		}
		if r.ManualUserID != nil && r.OverrideStart != nil && r.OverrideEnd != nil {
			// Full override: return it only when the time window is active.
			if now.After(*r.OverrideStart) && now.Before(*r.OverrideEnd) {
				cp := *r
				return &cp, nil
			}
			// Expired full override — skip; do not use as fallback.
			continue
		}
		// Normal rotation (at least one override field is nil): candidate fallback.
		if fallback == nil {
			cp := *r
			fallback = &cp
		}
	}
	if fallback != nil {
		return fallback, nil
	}
	return nil, store.ErrNotFound
}
