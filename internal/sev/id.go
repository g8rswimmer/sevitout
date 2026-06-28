package sev

import "fmt"

// FormatID returns a human-readable SEV ID from the year and sequence number.
// Example: FormatID(2026, 42) → "SEV-2026-0042"
func FormatID(year int, seq int64) string {
	return fmt.Sprintf("SEV-%d-%04d", year, seq)
}
