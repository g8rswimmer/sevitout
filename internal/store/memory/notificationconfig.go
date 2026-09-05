package memory

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/g8rswimmer/sevitout/internal/store"
)

// NotificationConfigStore is an in-memory implementation of
// store.NotificationConfigStore. Rules are keyed by ID (see
// store.NotificationConfig's doc comment for why a natural key no longer
// works once a rule can cover several events).
type NotificationConfigStore struct {
	mu   sync.RWMutex
	data map[int64]*store.NotificationConfig
	seq  atomic.Int64
}

func NewNotificationConfigStore() *NotificationConfigStore {
	return &NotificationConfigStore{data: make(map[int64]*store.NotificationConfig)}
}

var _ store.NotificationConfigStore = (*NotificationConfigStore)(nil)

func (s *NotificationConfigStore) Create(_ context.Context, cfg *store.NotificationConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg.ID = s.seq.Add(1)
	cp := *cfg
	cp.Events = append([]string(nil), cfg.Events...)
	s.data[cfg.ID] = &cp
	return nil
}

func (s *NotificationConfigStore) Update(_ context.Context, cfg *store.NotificationConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.data[cfg.ID]
	if !ok {
		return store.ErrNotFound
	}
	cfg.CreatedAt = existing.CreatedAt
	cp := *cfg
	cp.Events = append([]string(nil), cfg.Events...)
	s.data[cfg.ID] = &cp
	return nil
}

func (s *NotificationConfigStore) Delete(_ context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[id]; !ok {
		return store.ErrNotFound
	}
	delete(s.data, id)
	return nil
}

func (s *NotificationConfigStore) List(_ context.Context) ([]*store.NotificationConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*store.NotificationConfig, 0, len(s.data))
	for _, c := range s.data {
		cp := *c
		cp.Events = append([]string(nil), c.Events...)
		out = append(out, &cp)
	}
	return out, nil
}

func (s *NotificationConfigStore) ListForEvent(_ context.Context, event string, severityLevel *int16) ([]*store.NotificationConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*store.NotificationConfig
	for _, c := range s.data {
		if !containsEvent(c.Events, event) {
			continue
		}
		if c.MaxSeverityLevel != nil && severityLevel != nil && *c.MaxSeverityLevel < *severityLevel {
			continue
		}
		cp := *c
		cp.Events = append([]string(nil), c.Events...)
		out = append(out, &cp)
	}
	return out, nil
}

func containsEvent(events []string, event string) bool {
	for _, e := range events {
		if e == event {
			return true
		}
	}
	return false
}
