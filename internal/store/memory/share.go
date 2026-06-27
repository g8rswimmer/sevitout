package memory

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/g8rswimmer/sevitout/internal/store"
)

// ShareStore is an in-memory implementation of store.ShareStore.
type ShareStore struct {
	mu   sync.RWMutex
	data map[string]*store.ShareableLink // keyed by token
	seq  atomic.Int64
}

func NewShareStore() *ShareStore {
	return &ShareStore{data: make(map[string]*store.ShareableLink)}
}

var _ store.ShareStore = (*ShareStore)(nil)

func (s *ShareStore) Create(_ context.Context, link *store.ShareableLink) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.data[link.Token]; exists {
		return store.ErrConflict
	}
	link.ID = s.seq.Add(1)
	cp := *link
	s.data[link.Token] = &cp
	return nil
}

func (s *ShareStore) GetByToken(_ context.Context, token string) (*store.ShareableLink, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	link, ok := s.data[token]
	if !ok {
		return nil, store.ErrNotFound
	}
	cp := *link
	return &cp, nil
}

func (s *ShareStore) Revoke(_ context.Context, token string, revokedBy string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	link, ok := s.data[token]
	if !ok {
		return store.ErrNotFound
	}
	if link.Revoked {
		return nil
	}
	now := time.Now()
	link.Revoked = true
	link.RevokedBy = &revokedBy
	link.RevokedAt = &now
	return nil
}

func (s *ShareStore) ListBySEVID(_ context.Context, sevID string) ([]*store.ShareableLink, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*store.ShareableLink
	for _, link := range s.data {
		if link.SEVID == sevID {
			cp := *link
			out = append(out, &cp)
		}
	}
	return out, nil
}
