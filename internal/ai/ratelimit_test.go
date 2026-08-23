package ai

import (
	"testing"
	"time"
)

func TestRateLimiter_UnlimitedWhenZero(t *testing.T) {
	r := NewRateLimiter()
	for i := 0; i < 100; i++ {
		if !r.Allow(1, 0) {
			t.Fatalf("call %d: want allowed (limit 0 means unlimited)", i)
		}
	}
}

func TestRateLimiter_BurstAllowedUpToLimit(t *testing.T) {
	r := NewRateLimiter()
	for i := 0; i < 3; i++ {
		if !r.Allow(1, 3) {
			t.Fatalf("call %d: want allowed within limit", i)
		}
	}
	if r.Allow(1, 3) {
		t.Fatal("4th call: want denied, limit is 3/min")
	}
}

func TestRateLimiter_IndependentPerPlugin(t *testing.T) {
	r := NewRateLimiter()
	for i := 0; i < 2; i++ {
		if !r.Allow(1, 2) {
			t.Fatalf("plugin 1 call %d: want allowed", i)
		}
	}
	if r.Allow(1, 2) {
		t.Fatal("plugin 1: want denied after exhausting its limit")
	}
	if !r.Allow(2, 2) {
		t.Fatal("plugin 2: want allowed, independent window from plugin 1")
	}
}

func TestRateLimiter_ResetsAfterWindow(t *testing.T) {
	now := time.Now()
	r := NewRateLimiter()
	r.now = func() time.Time { return now }

	if !r.Allow(1, 1) {
		t.Fatal("first call: want allowed")
	}
	if r.Allow(1, 1) {
		t.Fatal("second call within same window: want denied")
	}

	now = now.Add(time.Minute + time.Second)
	if !r.Allow(1, 1) {
		t.Fatal("call after window elapsed: want allowed")
	}
}
