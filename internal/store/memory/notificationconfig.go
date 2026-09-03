package memory

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/g8rswimmer/sevitout/internal/store"
)

// notificationConfigKey identifies one routing rule.
type notificationConfigKey struct {
	role        store.OrgRole
	event       string
	channelType store.NotificationChannelType
}

// NotificationConfigStore is an in-memory implementation of
// store.NotificationConfigStore.
type NotificationConfigStore struct {
	mu   sync.RWMutex
	data map[notificationConfigKey]*store.NotificationConfig
	seq  atomic.Int64
}

func NewNotificationConfigStore() *NotificationConfigStore {
	return &NotificationConfigStore{data: make(map[notificationConfigKey]*store.NotificationConfig)}
}

var _ store.NotificationConfigStore = (*NotificationConfigStore)(nil)

func (s *NotificationConfigStore) Upsert(_ context.Context, cfg *store.NotificationConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := notificationConfigKey{cfg.Role, cfg.Event, cfg.ChannelType}
	if existing, ok := s.data[key]; ok {
		cfg.ID = existing.ID
		cfg.CreatedAt = existing.CreatedAt
	} else {
		cfg.ID = s.seq.Add(1)
	}
	cp := *cfg
	s.data[key] = &cp
	return nil
}

func (s *NotificationConfigStore) Delete(_ context.Context, role store.OrgRole, event string, channelType store.NotificationChannelType) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := notificationConfigKey{role, event, channelType}
	if _, ok := s.data[key]; !ok {
		return store.ErrNotFound
	}
	delete(s.data, key)
	return nil
}

func (s *NotificationConfigStore) List(_ context.Context) ([]*store.NotificationConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*store.NotificationConfig, 0, len(s.data))
	for _, c := range s.data {
		cp := *c
		out = append(out, &cp)
	}
	return out, nil
}

func (s *NotificationConfigStore) ListForEvent(_ context.Context, event string, severityLevel *int16) ([]*store.NotificationConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*store.NotificationConfig
	for _, c := range s.data {
		if c.Event != event {
			continue
		}
		if c.MaxSeverityLevel != nil && severityLevel != nil && *c.MaxSeverityLevel < *severityLevel {
			continue
		}
		cp := *c
		out = append(out, &cp)
	}
	return out, nil
}
