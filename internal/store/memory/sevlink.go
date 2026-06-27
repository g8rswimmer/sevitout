package memory

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/g8rswimmer/sevitout/internal/store"
)

// SEVLinkStore is an in-memory implementation of store.SEVLinkStore.
type SEVLinkStore struct {
	mu   sync.RWMutex
	data []*store.SEVLink
	seq  atomic.Int64
}

func NewSEVLinkStore() *SEVLinkStore { return &SEVLinkStore{} }

var _ store.SEVLinkStore = (*SEVLinkStore)(nil)

func (s *SEVLinkStore) Create(_ context.Context, link *store.SEVLink) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.data {
		if existing.SourceSEVID == link.SourceSEVID &&
			existing.TargetSEVID == link.TargetSEVID &&
			existing.RelationshipType == link.RelationshipType {
			return store.ErrConflict
		}
	}
	link.ID = s.seq.Add(1)
	cp := *link
	s.data = append(s.data, &cp)
	return nil
}

func (s *SEVLinkStore) Delete(_ context.Context, sourceSEVID, targetSEVID string, relType store.SEVRelationshipType) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, link := range s.data {
		if link.SourceSEVID == sourceSEVID &&
			link.TargetSEVID == targetSEVID &&
			link.RelationshipType == relType {
			s.data = append(s.data[:i], s.data[i+1:]...)
			return nil
		}
	}
	return store.ErrNotFound
}

func (s *SEVLinkStore) ListBySEVID(_ context.Context, sevID string) ([]*store.SEVLink, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*store.SEVLink
	for _, link := range s.data {
		if link.SourceSEVID == sevID || link.TargetSEVID == sevID {
			cp := *link
			out = append(out, &cp)
		}
	}
	return out, nil
}
