package memory

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/g8rswimmer/sevitout/internal/store"
)

// RoleStore is an in-memory implementation of store.RoleStore, used in unit tests.
type RoleStore struct {
	mu   sync.RWMutex
	seq  atomic.Int64
	data []*store.SEVRole
}

var _ store.RoleStore = (*RoleStore)(nil)

// NewRoleStore returns an empty RoleStore.
func NewRoleStore() *RoleStore { return &RoleStore{} }

func (s *RoleStore) Assign(_ context.Context, role *store.SEVRole) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	role.ID = s.seq.Add(1)
	cp := *role
	s.data = append(s.data, &cp)
	return nil
}

func (s *RoleStore) Remove(_ context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, r := range s.data {
		if r.ID == id {
			s.data = append(s.data[:i], s.data[i+1:]...)
			return nil
		}
	}
	return store.ErrNotFound
}

func (s *RoleStore) ListBySEVID(_ context.Context, sevID string) ([]*store.SEVRole, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*store.SEVRole
	for _, r := range s.data {
		if r.SEVID == sevID {
			cp := *r
			out = append(out, &cp)
		}
	}
	return out, nil
}
