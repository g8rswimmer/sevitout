package memory

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/g8rswimmer/sevitout/internal/store"
)

// ChatStore is an in-memory implementation of store.ChatStore.
type ChatStore struct {
	mu   sync.RWMutex
	data []*store.ChatEntry
	seq  atomic.Int64
}

func NewChatStore() *ChatStore { return &ChatStore{} }

var _ store.ChatStore = (*ChatStore)(nil)

func (s *ChatStore) Create(_ context.Context, entry *store.ChatEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry.ID = s.seq.Add(1)
	cp := *entry
	s.data = append(s.data, &cp)
	return nil
}

func (s *ChatStore) ListBySEVID(_ context.Context, sevID string) ([]*store.ChatEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*store.ChatEntry
	for _, e := range s.data {
		if e.SEVID == sevID {
			cp := *e
			out = append(out, &cp)
		}
	}
	return out, nil
}
