package ai

import (
	"sync"
	"time"
)

// RateLimiter enforces a per-plugin requests-per-minute ceiling (§11.3).
// It is a fixed-window counter, not a token bucket: simple, and sufficient
// for the coarse "N per minute" limits this system configures — a plugin
// that bursts N requests right at a window boundary can briefly exceed N in
// a 60s span, which is an acceptable trade for the simplicity here.
type RateLimiter struct {
	now func() time.Time

	mu   sync.Mutex
	wins map[int64]*window
}

type window struct {
	start time.Time
	count int
}

// NewRateLimiter returns a RateLimiter using the real wall clock.
func NewRateLimiter() *RateLimiter {
	return &RateLimiter{now: time.Now, wins: make(map[int64]*window)}
}

// Allow reports whether pluginID may make one more request right now, given
// limitPerMinute (0 means unlimited, always allowed). On true, the request
// is counted against the current window immediately — callers must not call
// Allow speculatively without following through on the request.
func (r *RateLimiter) Allow(pluginID int64, limitPerMinute int32) bool {
	if limitPerMinute <= 0 {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	w := r.wins[pluginID]
	if w == nil || now.Sub(w.start) >= time.Minute {
		w = &window{start: now}
		r.wins[pluginID] = w
	}
	if w.count >= int(limitPerMinute) {
		return false
	}
	w.count++
	return true
}
