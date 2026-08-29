package sev_test

import (
	"testing"

	"github.com/g8rswimmer/sevitout/internal/sev"
)

func TestFormatID(t *testing.T) {
	cases := []struct {
		name string
		year int
		seq  int64
		want string
	}{
		{"single digit seq is zero-padded to 4", 2026, 42, "SEV-2026-0042"},
		{"seq beyond 4 digits is not truncated", 2026, 12345, "SEV-2026-12345"},
		{"seq of 1", 2026, 1, "SEV-2026-0001"},
		{"seq of 0", 2026, 0, "SEV-2026-0000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sev.FormatID(tc.year, tc.seq)
			if got != tc.want {
				t.Errorf("FormatID(%d, %d) = %q, want %q", tc.year, tc.seq, got, tc.want)
			}
		})
	}
}
