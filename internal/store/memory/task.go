package memory

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/g8rswimmer/sevitout/internal/store"
)

// TaskStore is an in-memory implementation of store.TaskStore.
type TaskStore struct {
	mu   sync.RWMutex
	data map[int64]*store.LinkedTask
	seq  atomic.Int64
}

func NewTaskStore() *TaskStore {
	return &TaskStore{data: make(map[int64]*store.LinkedTask)}
}

var _ store.TaskStore = (*TaskStore)(nil)

func (s *TaskStore) Create(_ context.Context, task *store.LinkedTask) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	task.ID = s.seq.Add(1)
	cp := *task
	s.data[task.ID] = &cp
	return nil
}

func (s *TaskStore) Get(_ context.Context, id int64) (*store.LinkedTask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.data[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	cp := *t
	return &cp, nil
}

func (s *TaskStore) Update(_ context.Context, task *store.LinkedTask) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[task.ID]; !ok {
		return store.ErrNotFound
	}
	cp := *task
	s.data[task.ID] = &cp
	return nil
}

func (s *TaskStore) Delete(_ context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[id]; !ok {
		return store.ErrNotFound
	}
	delete(s.data, id)
	return nil
}

func (s *TaskStore) ListBySEVID(_ context.Context, sevID string) ([]*store.LinkedTask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*store.LinkedTask
	for _, t := range s.data {
		if t.SEVID == sevID {
			cp := *t
			out = append(out, &cp)
		}
	}
	return out, nil
}
