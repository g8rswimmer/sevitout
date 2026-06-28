package memory

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/g8rswimmer/sevitout/internal/store"
)

// SEVStore is an in-memory implementation of store.SEVStore, used in unit tests.
type SEVStore struct {
	mu   sync.RWMutex
	seq  atomic.Int64
	data map[string]*store.SEV
}

// NewSEVStore returns an empty SEVStore.
func NewSEVStore() *SEVStore {
	return &SEVStore{data: make(map[string]*store.SEV)}
}

var _ store.SEVStore = (*SEVStore)(nil)

func (s *SEVStore) Create(_ context.Context, sev *store.SEV) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sev.ID = fmt.Sprintf("SEV-%d-%04d", time.Now().UTC().Year(), s.seq.Add(1))
	cp := *sev
	s.data[sev.ID] = &cp
	return nil
}

func (s *SEVStore) Get(_ context.Context, id string) (*store.SEV, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sev, ok := s.data[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	cp := *sev
	return &cp, nil
}

func (s *SEVStore) Update(_ context.Context, sev *store.SEV) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[sev.ID]; !ok {
		return store.ErrNotFound
	}
	cp := *sev
	s.data[sev.ID] = &cp
	return nil
}

func (s *SEVStore) List(_ context.Context, filter store.SEVFilter) ([]*store.SEV, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var out []*store.SEV
	for _, sev := range s.data {
		if !matchesSEVFilter(sev, filter) {
			continue
		}
		cp := *sev
		out = append(out, &cp)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})

	if filter.Offset >= len(out) {
		return []*store.SEV{}, nil
	}
	out = out[filter.Offset:]
	if filter.Limit > 0 && filter.Limit < len(out) {
		out = out[:filter.Limit]
	}
	return out, nil
}

func (s *SEVStore) UpdateLocked(_ context.Context, id string, locked bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sev, ok := s.data[id]
	if !ok {
		return store.ErrNotFound
	}
	sev.Locked = locked
	return nil
}

func (s *SEVStore) Count(_ context.Context, filter store.SEVFilter) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	for _, sev := range s.data {
		if matchesSEVFilter(sev, filter) {
			n++
		}
	}
	return n, nil
}

func matchesSEVFilter(sev *store.SEV, f store.SEVFilter) bool {
	if len(f.SeverityLevels) > 0 {
		found := false
		for _, lvl := range f.SeverityLevels {
			if sev.SeverityLevel == lvl {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if len(f.Statuses) > 0 {
		found := false
		for _, st := range f.Statuses {
			if sev.Status == st {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if f.Search != "" {
		q := strings.ToLower(f.Search)
		haystack := strings.ToLower(sev.Title + " " + sev.Description)
		if !strings.Contains(haystack, q) {
			return false
		}
	}
	return true
}

// StatusHistoryStore is an in-memory implementation for SEV status history.
type StatusHistoryStore struct {
	mu   sync.RWMutex
	data []*store.SEVStatusHistory
	seq  atomic.Int64
}

var _ store.StatusHistoryStore = (*StatusHistoryStore)(nil)

func NewStatusHistoryStore() *StatusHistoryStore { return &StatusHistoryStore{} }

func (s *StatusHistoryStore) Create(_ context.Context, h *store.SEVStatusHistory) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	h.ID = s.seq.Add(1)
	cp := *h
	s.data = append(s.data, &cp)
	return nil
}

func (s *StatusHistoryStore) ListBySEVID(_ context.Context, sevID string) ([]*store.SEVStatusHistory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*store.SEVStatusHistory
	for _, h := range s.data {
		if h.SEVID == sevID {
			cp := *h
			out = append(out, &cp)
		}
	}
	return out, nil
}
