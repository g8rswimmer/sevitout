package memory

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/g8rswimmer/sevitout/internal/store"
)

// AnnouncementStore is an in-memory implementation of store.AnnouncementStore.
type AnnouncementStore struct {
	mu   sync.RWMutex
	data []*store.Announcement
	seq  atomic.Int64
}

func NewAnnouncementStore() *AnnouncementStore { return &AnnouncementStore{} }

var _ store.AnnouncementStore = (*AnnouncementStore)(nil)

func (s *AnnouncementStore) Create(_ context.Context, a *store.Announcement) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a.ID = s.seq.Add(1)
	cp := *a
	s.data = append(s.data, &cp)
	return nil
}

func (s *AnnouncementStore) ListBySEVID(_ context.Context, sevID string) ([]*store.Announcement, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*store.Announcement
	for _, a := range s.data {
		if a.SEVID == sevID {
			cp := *a
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (s *AnnouncementStore) SearchSEVIDs(_ context.Context, query string) ([]string, error) {
	if query == "" {
		return nil, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	q := strings.ToLower(query)
	seen := make(map[string]bool)
	// A non-nil (possibly empty) slice distinguishes "queried, zero matches"
	// from the query=="" case above ("no query").
	out := make([]string, 0)
	for _, a := range s.data {
		if !strings.Contains(strings.ToLower(a.Message), q) {
			continue
		}
		if !seen[a.SEVID] {
			seen[a.SEVID] = true
			out = append(out, a.SEVID)
		}
	}
	return out, nil
}
