package memory

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/g8rswimmer/sevitout/internal/store"
)

// serviceLevelingCriteriaKey identifies one row: a service at one severity level.
type serviceLevelingCriteriaKey struct {
	serviceID     string
	severityLevel int16
}

// ServiceLevelingCriteriaStore is an in-memory implementation of
// store.ServiceLevelingCriteriaStore.
type ServiceLevelingCriteriaStore struct {
	mu   sync.RWMutex
	data map[serviceLevelingCriteriaKey]*store.ServiceLevelingCriteria
	seq  atomic.Int64
}

func NewServiceLevelingCriteriaStore() *ServiceLevelingCriteriaStore {
	return &ServiceLevelingCriteriaStore{data: make(map[serviceLevelingCriteriaKey]*store.ServiceLevelingCriteria)}
}

var _ store.ServiceLevelingCriteriaStore = (*ServiceLevelingCriteriaStore)(nil)

func (s *ServiceLevelingCriteriaStore) Upsert(_ context.Context, c *store.ServiceLevelingCriteria) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := serviceLevelingCriteriaKey{c.ServiceID, c.SeverityLevel}
	if existing, ok := s.data[key]; ok {
		c.ID = existing.ID
		c.CreatedAt = existing.CreatedAt
	} else {
		c.ID = s.seq.Add(1)
	}
	cp := *c
	s.data[key] = &cp
	return nil
}

func (s *ServiceLevelingCriteriaStore) Get(_ context.Context, serviceID string, severityLevel int16) (*store.ServiceLevelingCriteria, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.data[serviceLevelingCriteriaKey{serviceID, severityLevel}]
	if !ok {
		return nil, store.ErrNotFound
	}
	cp := *c
	return &cp, nil
}

func (s *ServiceLevelingCriteriaStore) Delete(_ context.Context, serviceID string, severityLevel int16) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := serviceLevelingCriteriaKey{serviceID, severityLevel}
	if _, ok := s.data[key]; !ok {
		return store.ErrNotFound
	}
	delete(s.data, key)
	return nil
}

func (s *ServiceLevelingCriteriaStore) ListByService(_ context.Context, serviceID string) ([]*store.ServiceLevelingCriteria, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*store.ServiceLevelingCriteria, 0, 4)
	for level := int16(1); level <= 4; level++ {
		if c, ok := s.data[serviceLevelingCriteriaKey{serviceID, level}]; ok {
			cp := *c
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (s *ServiceLevelingCriteriaStore) ListForServices(_ context.Context, serviceIDs []string, severityLevel int16) ([]*store.ServiceLevelingCriteria, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*store.ServiceLevelingCriteria
	for _, id := range serviceIDs {
		if c, ok := s.data[serviceLevelingCriteriaKey{id, severityLevel}]; ok {
			cp := *c
			out = append(out, &cp)
		}
	}
	return out, nil
}
