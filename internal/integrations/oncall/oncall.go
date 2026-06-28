package oncall

import "context"

// OnCaller retrieves the current on-call person for a service.
// Implementations must return ("", nil) when no one is on-call.
type OnCaller interface {
	OnCallLookup(ctx context.Context, serviceID string) (string, error)
}
