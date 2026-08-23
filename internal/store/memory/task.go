package memory

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/g8rswimmer/sevitout/internal/store"
)

type taskKey struct {
	sevID          string
	externalSystem string
	taskID         string
}

// TaskStore is an in-memory implementation of store.TaskStore.
type TaskStore struct {
	mu    sync.RWMutex
	data  map[int64]*store.LinkedTask
	byKey map[taskKey]int64
	seq   atomic.Int64
}

func NewTaskStore() *TaskStore {
	return &TaskStore{
		data:  make(map[int64]*store.LinkedTask),
		byKey: make(map[taskKey]int64),
	}
}

var _ store.TaskStore = (*TaskStore)(nil)

func (s *TaskStore) Create(_ context.Context, task *store.LinkedTask) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := taskKey{task.SEVID, task.ExternalSystem, task.TaskID}
	if _, exists := s.byKey[key]; exists {
		return store.ErrConflict
	}
	task.ID = s.seq.Add(1)
	cp := *task
	s.data[task.ID] = &cp
	s.byKey[key] = task.ID
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

// SetDueDateIfUnset atomically sets a task's due date only if it doesn't
// already have one, and reports whether the write was applied. This lets
// callers back-fill a due date without racing concurrent callers doing the
// same back-fill for the same task.
func (s *TaskStore) SetDueDateIfUnset(_ context.Context, id int64, dueDate time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.data[id]
	if !ok {
		return false, store.ErrNotFound
	}
	if t.DueDate != nil {
		return false, nil
	}
	cp := *t
	cp.DueDate = &dueDate
	s.data[id] = &cp
	return true, nil
}

func (s *TaskStore) Delete(_ context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.data[id]
	if !ok {
		return store.ErrNotFound
	}
	delete(s.data, id)
	delete(s.byKey, taskKey{t.SEVID, t.ExternalSystem, t.TaskID})
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

func (s *TaskStore) CountOverdue(_ context.Context, now time.Time) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	for _, t := range s.data {
		if t.DueDate != nil && t.DueDate.Before(now) {
			n++
		}
	}
	return n, nil
}
