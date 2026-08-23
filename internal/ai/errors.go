package ai

import "errors"

// Sentinel errors returned by Dispatcher.Run / the proactive dispatch path.
// internal/api/grpc maps these to specific gRPC status codes.
var (
	ErrAIDisabledForSEV        = errors.New("ai: disabled for this SEV")
	ErrPluginDisabled          = errors.New("ai: plugin is disabled")
	ErrNoEnabledPlugin         = errors.New("ai: no enabled plugin configured")
	ErrRateLimited             = errors.New("ai: plugin rate limit exceeded")
	ErrEncryptionNotConfigured = errors.New("ai: encryption is not configured (ENCRYPTION_KEY not set), cannot decrypt plugin API key")
	ErrUnknownAction           = errors.New("ai: unknown action")
	ErrUnsupportedHandlerType  = errors.New("ai: unsupported plugin handler type")
)
