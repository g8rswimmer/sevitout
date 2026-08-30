package grpc

import (
	"context"

	"github.com/g8rswimmer/sevitout/internal/api/pb"
	"github.com/g8rswimmer/sevitout/internal/auth"
	"github.com/g8rswimmer/sevitout/internal/store"
)

// Encryptor encrypts and decrypts integration credentials at rest with
// AES-256-GCM (see internal/store/crypto). Declared here (the consumer) so
// this package depends only on the two operations it needs; crypto.KeyEncryptor
// satisfies this implicitly.
type Encryptor interface {
	Encrypt(plaintext []byte) ([]byte, error)
	Decrypt(ciphertext []byte) ([]byte, error)
}

// RateLimitEvictor drops a deleted AI plugin's rate-limit window so the
// limiter doesn't retain an entry for it forever. Declared here (the
// consumer) per this repo's interface-ownership convention — ai.Dispatcher
// satisfies it implicitly via EvictRateLimit.
type RateLimitEvictor interface {
	EvictRateLimit(pluginID int64)
}

// IntegrationCredentialsRefresher is handed the plaintext credentials and
// settings UpsertIntegrationConfig is about to persist for one
// integrationType, so an in-process client cached from that integration's
// credentials (see cmd/server's OnCaller/IssueClient/JiraIssueClient
// *Resolver types) can apply them immediately — without waiting for a
// server restart, and without ever needing to read or decrypt anything
// from the datastore itself; that only happens once, at that resolver's own
// startup. Declared here (the consumer) per this repo's interface-ownership
// convention. Implementations must ignore calls for an integrationType they
// don't own (returning nil) and must be safe for concurrent use.
//
// A non-nil error means credentials/settings could not be turned into a
// usable client. UpsertIntegrationConfig calls every registered refresher
// before persisting anything, so an error here means the write is rejected
// outright — nothing is saved — rather than a config being confirmed as
// saved when it silently isn't usable.
type IntegrationCredentialsRefresher interface {
	RefreshIntegrationCredentials(ctx context.Context, integrationType string, credentials map[string]string, settings map[string]any) error
}

// ConfigServer implements pb.ConfigServiceServer: the admin configuration API
// (service registry, user management, on-call rotations, integration
// credentials, AI plugin registration, and data retention policy). Its
// methods are split across this file and, by domain, config_service.go,
// config_user.go, config_oncall.go, config_integration.go, config_ai.go, and
// config_retention.go.
type ConfigServer struct {
	pb.UnimplementedConfigServiceServer
	services     store.ServiceStore
	users        store.UserStore
	oncall       store.OnCallStore
	integrations store.IntegrationConfigStore
	retention    store.RetentionConfigStore
	aiPlugins    store.AIPluginStore
	crypto       Encryptor                         // nil when ENCRYPTION_KEY is not set
	rateLimits   RateLimitEvictor                  // nil is a no-op (e.g. in tests that don't wire a Dispatcher)
	refreshers   []IntegrationCredentialsRefresher // notified after every successful UpsertIntegrationConfig
}

// ConfigServerParams groups NewConfigServer's dependencies. Crypto may be
// nil, in which case UpsertIntegrationConfig and CreateAIPlugin/
// UpdateAIPlugin reject any request that supplies credentials/an API key.
// RateLimits and Refreshers may also be nil/empty.
type ConfigServerParams struct {
	Services     store.ServiceStore
	Users        store.UserStore
	OnCall       store.OnCallStore
	Integrations store.IntegrationConfigStore
	Retention    store.RetentionConfigStore
	AIPlugins    store.AIPluginStore
	Crypto       Encryptor
	RateLimits   RateLimitEvictor
	Refreshers   []IntegrationCredentialsRefresher
}

func NewConfigServer(p ConfigServerParams) *ConfigServer {
	return &ConfigServer{
		services:     p.Services,
		users:        p.Users,
		oncall:       p.OnCall,
		integrations: p.Integrations,
		retention:    p.Retention,
		aiPlugins:    p.AIPlugins,
		crypto:       p.Crypto,
		rateLimits:   p.RateLimits,
		refreshers:   p.Refreshers,
	}
}

func callerID(ctx context.Context) string {
	if uc, ok := auth.UserFromContext(ctx); ok {
		return uc.UserID
	}
	return ""
}
