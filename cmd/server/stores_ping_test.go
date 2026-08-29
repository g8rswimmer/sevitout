package main

import (
	"context"
	"testing"
)

func TestStores_Ping_NilPool_IsNoop(t *testing.T) {
	// The in-memory dev fallback (DATABASE_URL unset) leaves Pool nil — there
	// is no connection to check, so Ping must always succeed rather than nil
	// panicking on Pool.Ping.
	s := &Stores{}
	if err := s.Ping(context.Background()); err != nil {
		t.Errorf("Ping with nil Pool = %v, want nil", err)
	}
}
