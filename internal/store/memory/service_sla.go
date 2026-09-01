package memory

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/g8rswimmer/sevitout/internal/store"
)

// serviceSLAKey identifies one row: a service at one severity level.
type serviceSLAKey struct {
	serviceID     string
	severityLevel int16
}

// ServiceSLAStore is an in-memory implementation of store.ServiceSLAStore.
type ServiceSLAStore struct {
	mu   sync.RWMutex
	data map[serviceSLAKey]*store.ServiceSLA
	seq  atomic.Int64
}

func NewServiceSLAStore() *ServiceSLAStore {
	return &ServiceSLAStore{data: make(map[serviceSLAKey]*store.ServiceSLA)}
}

var _ store.ServiceSLAStore = (*ServiceSLAStore)(nil)

func (s *ServiceSLAStore) Upsert(_ context.Context, sla *store.ServiceSLA) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := serviceSLAKey{sla.ServiceID, sla.SeverityLevel}
	if existing, ok := s.data[key]; ok {
		sla.ID = existing.ID
		sla.CreatedAt = existing.CreatedAt
	} else {
		sla.ID = s.seq.Add(1)
	}
	cp := *sla
	s.data[key] = &cp
	return nil
}

func (s *ServiceSLAStore) Get(_ context.Context, serviceID string, severityLevel int16) (*store.ServiceSLA, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sla, ok := s.data[serviceSLAKey{serviceID, severityLevel}]
	if !ok {
		return nil, store.ErrNotFound
	}
	cp := *sla
	return &cp, nil
}

func (s *ServiceSLAStore) Delete(_ context.Context, serviceID string, severityLevel int16) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := serviceSLAKey{serviceID, severityLevel}
	if _, ok := s.data[key]; !ok {
		return store.ErrNotFound
	}
	delete(s.data, key)
	return nil
}

func (s *ServiceSLAStore) ListByService(_ context.Context, serviceID string) ([]*store.ServiceSLA, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*store.ServiceSLA, 0, 4)
	for level := int16(1); level <= 4; level++ {
		if sla, ok := s.data[serviceSLAKey{serviceID, level}]; ok {
			cp := *sla
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (s *ServiceSLAStore) ListForServices(_ context.Context, serviceIDs []string, severityLevel int16) ([]*store.ServiceSLA, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*store.ServiceSLA
	for _, id := range serviceIDs {
		if sla, ok := s.data[serviceSLAKey{id, severityLevel}]; ok {
			cp := *sla
			out = append(out, &cp)
		}
	}
	return out, nil
}
