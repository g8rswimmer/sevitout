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

func (s *RoleStore) Remove(_ context.Context, sevID string, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, r := range s.data {
		if r.ID == id && r.SEVID == sevID {
			last := len(s.data) - 1
			s.data[i] = s.data[last]
			s.data[last] = nil
			s.data = s.data[:last]
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

func (s *RoleStore) ListSEVIDsByUser(_ context.Context, user string, roleType *store.SEVRoleType) ([]string, error) {
	if user == "" {
		return nil, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	seen := make(map[string]bool)
	// A non-nil (possibly empty) slice distinguishes "queried, zero matches"
	// from the user=="" case above ("no query"), which callers such as
	// intersectIDs in internal/api/grpc/search.go treat as unconstrained.
	out := make([]string, 0)
	for _, r := range s.data {
		if roleType != nil && r.RoleType != *roleType {
			continue
		}
		if (r.UserID == nil || *r.UserID != user) && r.DisplayName != user {
			continue
		}
		if !seen[r.SEVID] {
			seen[r.SEVID] = true
			out = append(out, r.SEVID)
		}
	}
	return out, nil
}
