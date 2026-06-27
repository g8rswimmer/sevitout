package memory

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/g8rswimmer/sevitout/internal/store"
)

// AuditStore is an in-memory, append-only implementation of store.AuditStore.
// It intentionally exposes no update or delete operations.
type AuditStore struct {
	mu   sync.RWMutex
	data []*store.AuditEntry
	seq  atomic.Int64
}

func NewAuditStore() *AuditStore { return &AuditStore{} }

var _ store.AuditStore = (*AuditStore)(nil)

func (s *AuditStore) Append(_ context.Context, entry *store.AuditEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry.ID = s.seq.Add(1)
	cp := *entry
	s.data = append(s.data, &cp)
	return nil
}

func (s *AuditStore) ListBySEVID(_ context.Context, sevID string) ([]*store.AuditEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*store.AuditEntry
	for _, e := range s.data {
		if e.SEVID == sevID {
			cp := *e
			out = append(out, &cp)
		}
	}
	return out, nil
}
