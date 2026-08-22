package memory

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/g8rswimmer/sevitout/internal/store"
)

// RetentionConfigStore is an in-memory implementation of store.RetentionConfigStore.
type RetentionConfigStore struct {
	mu   sync.RWMutex
	data map[int16]*store.RetentionConfig // keyed by severity_level
	seq  atomic.Int64
}

// NewRetentionConfigStore returns a RetentionConfigStore pre-seeded with the
// default "retain forever" policy for SEV-1 through SEV-4, mirroring the seed
// data in migrations/000002_schema.up.sql so behavior matches Postgres from
// a fresh start.
func NewRetentionConfigStore() *RetentionConfigStore {
	s := &RetentionConfigStore{data: make(map[int16]*store.RetentionConfig)}
	now := time.Now()
	for level := int16(1); level <= 4; level++ {
		s.seq.Add(1)
		s.data[level] = &store.RetentionConfig{
			ID:            s.seq.Load(),
			SeverityLevel: level,
			RetentionDays: 0,
			HardDelete:    false,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
	}
	return s
}

var _ store.RetentionConfigStore = (*RetentionConfigStore)(nil)

func (s *RetentionConfigStore) Get(_ context.Context, severityLevel int16) (*store.RetentionConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cfg, ok := s.data[severityLevel]
	if !ok {
		return nil, store.ErrNotFound
	}
	cp := *cfg
	return &cp, nil
}

func (s *RetentionConfigStore) Upsert(_ context.Context, cfg *store.RetentionConfig) error {
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

func (s *RetentionConfigStore) List(_ context.Context) ([]*store.RetentionConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*store.RetentionConfig, 0, len(s.data))
	for level := int16(1); level <= 4; level++ {
		if cfg, ok := s.data[level]; ok {
			cp := *cfg
			out = append(out, &cp)
		}
	}
	return out, nil
}
