package main

import (
	"context"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/g8rswimmer/sevitout/internal/api/pb"
	"github.com/g8rswimmer/sevitout/internal/api/ws"
	sevitoutslack "github.com/g8rswimmer/sevitout/internal/integrations/slack"
)

// slackSettingsRetryAttempts/Delay bound how long main waits for the API
// server to come up before giving up on loading the "slack" integration
// config at startup (docker-compose's depends_on only orders container
// start, it doesn't wait for the API to be ready to serve). Declared as vars
// (not consts) so tests can shrink the delay rather than waiting for real.
var (
	slackSettingsRetryAttempts = 5
	slackSettingsRetryDelay    = 2 * time.Second
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	// Every other integration in this repo (PagerDuty, GitHub) is optional
	// and disables itself gracefully when unconfigured. A Slack bot with no
	// Slack credentials has nothing to fall back to, so it exits cleanly
	// (code 0) rather than crash-looping — matching how `migrate` exits 0
	// once schema migrations are already applied. This keeps `make up`
	// quiet for every earlier milestone's demo, none of which configure
	// Slack, since docker-compose's default restart policy is "no".
	appToken, ok1 := optionalEnv(log, "SLACK_APP_TOKEN")
	botToken, ok2 := optionalEnv(log, "SLACK_BOT_TOKEN")
	apiAddr, ok3 := optionalEnv(log, "API_GRPC_ADDR")
	serviceEmail, ok4 := optionalEnv(log, "SLACKBOT_SERVICE_EMAIL")
	servicePassword, ok5 := optionalEnv(log, "SLACKBOT_SERVICE_PASSWORD")
	if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 {
		log.Warn("slackbot disabled: one or more required environment variables are not set")
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// tokenSource logs the bot in itself (POST /auth/login, same host:port
	// as the gRPC dial — the API server multiplexes both on one port) and
	// keeps its token fresh for as long as the bot runs, replacing a
	// manually pre-issued, manually rotated SLACKBOT_SERVICE_TOKEN.
	tokens := newTokenSource(tokenSourceParams{
		APIAddr: apiAddr, Email: serviceEmail, Password: servicePassword, Log: log,
	})

	conn, err := grpc.NewClient(apiAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithPerRPCCredentials(tokens),
		grpc.WithChainUnaryInterceptor(tokens.retryOnUnauthenticated),
	)
	if err != nil {
		log.Error("dial api server", "addr", apiAddr, "err", err)
		os.Exit(1)
	}
	defer func() { _ = conn.Close() }()

	api := apiClients{
		sevs:          pb.NewSEVServiceClient(conn),
		roles:         pb.NewRoleServiceClient(conn),
		announcements: pb.NewAnnouncementServiceClient(conn),
		chats:         pb.NewChatServiceClient(conn),
		config:        pb.NewConfigServiceClient(conn),
	}

	defaultChannel, namingConvention, restBotToken, _ := loadSlackSettings(ctx, log, api.config, botToken, appToken)

	// Socket Mode (slash commands, app-mention events) always uses the
	// static env-var tokens — live-reconnecting it on a datastore-configured
	// token change is an explicit follow-up (docs/roadmap.md Phase 8), not
	// built here. Only the REST client behind bot.slack (CreateChannel/
	// PostMessage/etc., wrapped in slackResolver below) prefers the
	// datastore-configured pair when one's available.
	slackAPI := slack.New(botToken, slack.OptionAppLevelToken(appToken))
	smClient := socketmode.New(slackAPI)

	slackResolver := newSlackClientResolver(sevitoutslack.NewClient(restBotToken), restBotToken)

	b := newBot(botParams{
		Slack: slackResolver, API: api, Log: log,
		DefaultChannel: defaultChannel, ChannelNamingConvention: namingConvention,
	})

	wsURL := (&url.URL{Scheme: "ws", Host: apiAddr, Path: "/ws", RawQuery: "sev_id=" + url.QueryEscape(ws.BroadcastRoom)}).String()
	go b.runEventListener(ctx, wsURL, tokens)
	go b.runSettingsRefresher(ctx, api.config, slackResolver)
	go tokens.runTokenRefresher(ctx)

	go runSocketMode(ctx, log, b, smClient)

	log.Info("slackbot starting", "api_addr", apiAddr)
	// RunContext only ever returns via its err != nil path (an infinite loop
	// otherwise), so that comparison is always true — check ctx.Err() alone
	// to distinguish a real failure from a requested shutdown.
	if err := smClient.RunContext(ctx); ctx.Err() == nil {
		log.Error("socket mode run failed", "err", err)
		os.Exit(1)
	}
}

// runSocketMode consumes smClient's event channel for as long as ctx is
// live, dispatching slash commands and app-mention events to b. Each
// dispatch runs in its own goroutine so a slow gRPC call (or a slow Slack
// API call for the reply) never delays acknowledging — or handling — the
// next incoming event.
func runSocketMode(ctx context.Context, log *slog.Logger, b *bot, smClient *socketmode.Client) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-smClient.Events:
			if !ok {
				return
			}
			switch evt.Type {
			case socketmode.EventTypeConnecting:
				log.Info("connecting to slack")
			case socketmode.EventTypeConnectionError:
				log.Error("slack connection error")
			case socketmode.EventTypeConnected:
				log.Info("connected to slack")
			case socketmode.EventTypeSlashCommand:
				cmd, ok := evt.Data.(slack.SlashCommand)
				if !ok {
					continue
				}
				_ = smClient.Ack(*evt.Request)
				go func() {
					reply := b.handleSlashCommand(ctx, cmd)
					if err := b.slack.PostMessage(ctx, cmd.ChannelID, reply); err != nil {
						log.Error("post slash command reply failed", "err", err)
					}
				}()
			case socketmode.EventTypeEventsAPI:
				eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
				if !ok {
					continue
				}
				_ = smClient.Ack(*evt.Request)
				if eventsAPIEvent.Type != slackevents.CallbackEvent {
					continue
				}
				if mention, ok := eventsAPIEvent.InnerEvent.Data.(*slackevents.AppMentionEvent); ok {
					go func() {
						reply := b.handleMention(ctx, mention.Text)
						if err := b.slack.PostMessage(ctx, mention.Channel, reply); err != nil {
							log.Error("post mention reply failed", "err", err)
						}
					}()
				}
			}
		}
	}
}

// loadSlackSettings fetches the "slack" integration config's non-secret
// settings (docs/requirements.md §18.4: default notification channel,
// incident channel naming convention) and, per docs/roadmap.md Phase 8,
// resolves the bot/app token pair the REST client should start with —
// preferring a datastore-configured credential over staticBotToken/
// staticAppToken (SLACK_BOT_TOKEN/SLACK_APP_TOKEN), the same
// datastore-preferred-with-env-fallback pattern PagerDuty/GitHub/Jira use in
// cmd/server. Retries briefly since the API server may still be starting
// up; on persistent failure it logs a warning and falls back to
// SLACK_DEFAULT_CHANNEL/SLACK_CHANNEL_NAMING_CONVENTION for the settings and
// the static tokens for the credential pair, rather than failing to start
// entirely.
func loadSlackSettings(ctx context.Context, log *slog.Logger, config configAPI, staticBotToken, staticAppToken string) (defaultChannel, namingConvention, botToken, appToken string) {
	var lastErr error
	for attempt := 1; attempt <= slackSettingsRetryAttempts; attempt++ {
		resp, err := config.GetIntegrationConfig(ctx, &pb.GetIntegrationConfigRequest{IntegrationType: "slack"})
		if err == nil {
			botToken, appToken = resolveSlackBotCredential(ctx, log, config, staticBotToken, staticAppToken)
			return resp.GetSettings()["default_channel"], resp.GetSettings()["channel_naming_convention"], botToken, appToken
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return "", "", staticBotToken, staticAppToken
		case <-time.After(slackSettingsRetryDelay):
		}
	}
	defaultChannel = os.Getenv("SLACK_DEFAULT_CHANNEL")
	namingConvention = os.Getenv("SLACK_CHANNEL_NAMING_CONVENTION")
	log.Warn("could not load slack integration config, using defaults", "err", lastErr, "default_channel", defaultChannel, "channel_naming_convention", namingConvention)
	return defaultChannel, namingConvention, staticBotToken, staticAppToken
}

// resolveSlackBotCredential returns the bot/app token pair the REST client
// (slackClientResolver) should use at startup: the datastore-configured pair
// via ConfigService.GetSlackBotCredential when one is available, falling
// back to staticBotToken/staticAppToken otherwise. A fetch error or an empty
// bot_token (no "slack" integration configured, or this server has no
// slackbot service account set up — see internal/config.Config's
// SlackbotServiceEmail) is treated as "nothing usable in the datastore," not
// a fatal error, since the static tokens remain a valid fallback.
func resolveSlackBotCredential(ctx context.Context, log *slog.Logger, config configAPI, staticBotToken, staticAppToken string) (botToken, appToken string) {
	resp, err := config.GetSlackBotCredential(ctx, &pb.GetSlackBotCredentialRequest{})
	if err != nil {
		log.Debug("no datastore-configured slack bot credential, using static tokens", "err", err)
		return staticBotToken, staticAppToken
	}
	if resp.GetBotToken() == "" {
		return staticBotToken, staticAppToken
	}
	return resp.GetBotToken(), resp.GetAppToken()
}

// optionalEnv reads name from the environment, logging which variable was
// missing so an operator can tell why the bot declined to start.
func optionalEnv(log *slog.Logger, name string) (value string, ok bool) {
	v := os.Getenv(name)
	if v == "" {
		log.Warn(name + " is not set")
		return "", false
	}
	return v, true
}
