package memory

import (
	"context"
	"sync"
	"time"

	"github.com/g8rswimmer/sevitout/internal/store"
)

// UserStore is an in-memory implementation of store.UserStore.
type UserStore struct {
	mu   sync.RWMutex
	data map[string]*store.User // keyed by id
}

func NewUserStore() *UserStore {
	return &UserStore{data: make(map[string]*store.User)}
}

var _ store.UserStore = (*UserStore)(nil)

func (s *UserStore) Create(_ context.Context, user *store.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.data[user.ID]; exists {
		return store.ErrConflict
	}
	for _, u := range s.data {
		if u.Email == user.Email {
			return store.ErrConflict
		}
	}
	cp := *user
	s.data[user.ID] = &cp
	return nil
}

func (s *UserStore) Get(_ context.Context, id string) (*store.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.data[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	cp := *u
	return &cp, nil
}

func (s *UserStore) GetByEmail(_ context.Context, email string) (*store.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.data {
		if u.Email == email {
			cp := *u
			return &cp, nil
		}
	}
	return nil, store.ErrNotFound
}

func (s *UserStore) Update(_ context.Context, user *store.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[user.ID]; !ok {
		return store.ErrNotFound
	}
	for _, u := range s.data {
		if u.ID != user.ID && u.Email == user.Email {
			return store.ErrConflict
		}
	}
	cp := *user
	s.data[user.ID] = &cp
	return nil
}

func (s *UserStore) List(_ context.Context) ([]*store.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*store.User, 0, len(s.data))
	for _, u := range s.data {
		cp := *u
		out = append(out, &cp)
	}
	return out, nil
}

func (s *UserStore) Count(_ context.Context) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return int64(len(s.data)), nil
}

// UpdateIntegrationIdentities full-replaces user's self-service integration
// identities (Slack user ID, GitHub username, Jira account ID).
func (s *UserStore) UpdateIntegrationIdentities(_ context.Context, userID string, slackUserID, githubUsername, jiraAccountID *string) (*store.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.data[userID]
	if !ok {
		return nil, store.ErrNotFound
	}
	cp := *u
	cp.SlackUserID = slackUserID
	cp.GitHubUsername = githubUsername
	cp.JiraAccountID = jiraAccountID
	cp.UpdatedAt = time.Now()
	s.data[userID] = &cp
	out := cp
	return &out, nil
}
