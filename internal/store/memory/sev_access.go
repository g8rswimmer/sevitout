package memory

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/g8rswimmer/sevitout/internal/store"
)

// SEVAccessStore is an in-memory implementation of store.SEVAccessStore,
// used in unit tests.
type SEVAccessStore struct {
	mu   sync.RWMutex
	seq  atomic.Int64
	data []*store.SEVAccess
}

var _ store.SEVAccessStore = (*SEVAccessStore)(nil)

// NewSEVAccessStore returns an empty SEVAccessStore.
func NewSEVAccessStore() *SEVAccessStore { return &SEVAccessStore{} }

func (s *SEVAccessStore) Grant(_ context.Context, access *store.SEVAccess) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.data {
		if a.SEVID == access.SEVID && a.UserID == access.UserID {
			return store.ErrConflict
		}
	}
	access.ID = s.seq.Add(1)
	cp := *access
	s.data = append(s.data, &cp)
	return nil
}

func (s *SEVAccessStore) Revoke(_ context.Context, sevID string, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, a := range s.data {
		if a.ID == id && a.SEVID == sevID {
			last := len(s.data) - 1
			s.data[i] = s.data[last]
			s.data[last] = nil
			s.data = s.data[:last]
			return nil
		}
	}
	return store.ErrNotFound
}

func (s *SEVAccessStore) ListBySEVID(_ context.Context, sevID string) ([]*store.SEVAccess, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*store.SEVAccess
	for _, a := range s.data {
		if a.SEVID == sevID {
			cp := *a
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (s *SEVAccessStore) HasAccess(_ context.Context, sevID, userID string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, a := range s.data {
		if a.SEVID == sevID && a.UserID == userID {
			return true, nil
		}
	}
	return false, nil
}

func (s *SEVAccessStore) ListSEVIDsByUser(_ context.Context, userID string) ([]string, error) {
	if userID == "" {
		return nil, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0)
	for _, a := range s.data {
		if a.UserID == userID {
			out = append(out, a.SEVID)
		}
	}
	return out, nil
}
