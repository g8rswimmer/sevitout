package memory

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/g8rswimmer/sevitout/internal/store"
)

// EscalationConfigStore is an in-memory implementation of
// store.EscalationConfigStore, pre-seeded with all four severity levels
// disabled — matching internal/store/memory/retentionconfig.go's
// pre-seeded-defaults precedent.
type EscalationConfigStore struct {
	mu   sync.RWMutex
	data map[int16]*store.EscalationConfig
	seq  atomic.Int64
}

func NewEscalationConfigStore() *EscalationConfigStore {
	s := &EscalationConfigStore{data: make(map[int16]*store.EscalationConfig)}
	now := time.Now()
	for level := int16(1); level <= 4; level++ {
		s.seq.Add(1)
		s.data[level] = &store.EscalationConfig{
			ID:               s.seq.Load(),
			SeverityLevel:    level,
			ThresholdMinutes: 0,
			Enabled:          false,
			CreatedAt:        now,
			UpdatedAt:        now,
		}
	}
	return s
}

var _ store.EscalationConfigStore = (*EscalationConfigStore)(nil)

func (s *EscalationConfigStore) Get(_ context.Context, severityLevel int16) (*store.EscalationConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.data[severityLevel]
	if !ok {
		return nil, store.ErrNotFound
	}
	cp := *c
	return &cp, nil
}

func (s *EscalationConfigStore) Upsert(_ context.Context, cfg *store.EscalationConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.data[cfg.SeverityLevel]; ok {
		cfg.ID = existing.ID
		cfg.CreatedAt = existing.CreatedAt
	} else {
		cfg.ID = s.seq.Add(1)
	}
	cp := *cfg
	s.data[cfg.SeverityLevel] = &cp
	return nil
}

func (s *EscalationConfigStore) List(_ context.Context) ([]*store.EscalationConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*store.EscalationConfig, 0, 4)
	for level := int16(1); level <= 4; level++ {
		if c, ok := s.data[level]; ok {
			cp := *c
			out = append(out, &cp)
		}
	}
	return out, nil
}
