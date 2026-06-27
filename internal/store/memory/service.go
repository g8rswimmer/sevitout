package memory

import (
	"context"
	"sync"

	"github.com/g8rswimmer/sevitout/internal/store"
)

// ServiceStore is an in-memory implementation of store.ServiceStore.
type ServiceStore struct {
	mu   sync.RWMutex
	data map[string]*store.Service // keyed by id
}

func NewServiceStore() *ServiceStore {
	return &ServiceStore{data: make(map[string]*store.Service)}
}

var _ store.ServiceStore = (*ServiceStore)(nil)

func (s *ServiceStore) Create(_ context.Context, svc *store.Service) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.data[svc.ID]; exists {
		return store.ErrConflict
	}
	for _, existing := range s.data {
		if existing.Name == svc.Name {
			return store.ErrConflict
		}
	}
	cp := *svc
	s.data[svc.ID] = &cp
	return nil
}

func (s *ServiceStore) Get(_ context.Context, id string) (*store.Service, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	svc, ok := s.data[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	cp := *svc
	return &cp, nil
}

func (s *ServiceStore) Update(_ context.Context, svc *store.Service) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[svc.ID]; !ok {
		return store.ErrNotFound
	}
	cp := *svc
	s.data[svc.ID] = &cp
	return nil
}

func (s *ServiceStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[id]; !ok {
		return store.ErrNotFound
	}
	delete(s.data, id)
	return nil
}

func (s *ServiceStore) List(_ context.Context, activeOnly bool) ([]*store.Service, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*store.Service
	for _, svc := range s.data {
		if activeOnly && !svc.Active {
			continue
		}
		cp := *svc
		out = append(out, &cp)
	}
	return out, nil
}
