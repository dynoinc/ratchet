package web

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/earthboundkid/versioninfo/v2"
	"github.com/jackc/pgx/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"riverqueue.com/riverui"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/docs"
	"github.com/dynoinc/ratchet/internal/llm"
	"github.com/dynoinc/ratchet/internal/modules/channel_monitor"
	"github.com/dynoinc/ratchet/internal/modules/commands"
	"github.com/dynoinc/ratchet/internal/modules/runbook"
	"github.com/dynoinc/ratchet/internal/slack_integration"
	"github.com/dynoinc/ratchet/internal/storage/schema"
)

func handleJSON(handler func(*http.Request) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		result, err := handler(r)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}

			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if result == nil {
			result = struct{}{}
		}

		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		w.Header().Set("Content-Type", "application/json")
		if err := encoder.Encode(result); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

type httpHandlers struct {
	bot              *internal.Bot
	commands         *commands.Commands
	slackIntegration slack_integration.Integration
	llmClient        llm.Client
	docsConfig       *docs.Config
}

func New(
	ctx context.Context,
	bot *internal.Bot,
	commands *commands.Commands,
	slackIntegration slack_integration.Integration,
	llmClient llm.Client,
	docsConfig *docs.Config,
) (http.Handler, error) {
	handlers := &httpHandlers{
		bot:              bot,
		commands:         commands,
		slackIntegration: slackIntegration,
		llmClient:        llmClient,
		docsConfig:       docsConfig,
	}

	// River UI
	opts := &riverui.ServerOpts{
		Client: bot.RiverClient,
		DB:     bot.DB,
		Prefix: "/riverui",
		Logger: slog.Default(),
	}
	riverServer, err := riverui.NewServer(opts)
	if err != nil {
		return nil, fmt.Errorf("creating riverui server: %w", err)
	}
	if err := riverServer.Start(ctx); err != nil {
		return nil, fmt.Errorf("starting riverui server: %w", err)
	}

	withoutTrailingSlash := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" && strings.HasSuffix(r.URL.Path, "/") {
				r.URL.Path = strings.TrimSuffix(r.URL.Path, "/")
				http.Redirect(w, r, r.URL.String(), http.StatusPermanentRedirect)
				return
			}

			next.ServeHTTP(w, r)
		})
	}

	apiMux := http.NewServeMux()

	// Channels
	apiMux.HandleFunc("GET /channels", handleJSON(handlers.listChannels))
	apiMux.HandleFunc("GET /channels/{channel_name}", handleJSON(handlers.getChannel))
	apiMux.HandleFunc("GET /channels/{channel_name}/messages", handleJSON(handlers.listMessages))
	apiMux.HandleFunc("POST /channels/{channel_name}/onboard", handleJSON(handlers.onboardChannel))
	apiMux.HandleFunc("POST /channels/{channel_name}/agent-mode", handleJSON(handlers.agentMode))

	// Services
	apiMux.HandleFunc("GET /services", handleJSON(handlers.listServices))
	apiMux.HandleFunc("GET /services/{service}/alerts", handleJSON(handlers.listAlerts))
	apiMux.HandleFunc("GET /services/{service}/alerts/{alert}/messages", handleJSON(handlers.listThreadMessages))
	apiMux.HandleFunc("GET /services/{service}/alerts/{alert}/runbook", handleJSON(handlers.getRunbook))
	apiMux.HandleFunc("POST /services/{service}/alerts/{alert}/post-runbook", handleJSON(handlers.postRunbook))

	// Commands
	apiMux.HandleFunc("GET /commands/generate", otelhttp.NewHandler(http.HandlerFunc(handlers.generateCommand), "commands-generate").ServeHTTP)
	apiMux.HandleFunc("POST /commands/respond", otelhttp.NewHandler(http.HandlerFunc(handlers.respondCommand), "commands-respond").ServeHTTP)

	// Documentation
	apiMux.HandleFunc("GET /docs/status", handleJSON(handlers.docsStatus))
	apiMux.HandleFunc("POST /docs/refresh", handleJSON(handlers.postRefresh))

	mux := http.NewServeMux()
	mux.Handle("/riverui/", riverServer)
	mux.Handle("/api/", withoutTrailingSlash(http.StripPrefix("/api", apiMux)))
	mux.Handle("/channelmonitor/", http.StripPrefix("/channelmonitor", channel_monitor.HTTPHandler(handlers.llmClient, handlers.slackIntegration, "/channelmonitor")))
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.Handle("GET /version", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(versioninfo.Short()))
	}))

	return mux, nil
}

func (h *httpHandlers) listChannels(r *http.Request) (any, error) {
	channels, err := schema.New(h.bot.DB).GetAllChannels(r.Context())
	if err != nil {
		return nil, err
	}

	sort.Slice(channels, func(i, j int) bool {
		return channels[i].Attrs.Name < channels[j].Attrs.Name
	})

	return channels, nil
}

func (h *httpHandlers) getChannel(r *http.Request) (any, error) {
	channelName := r.PathValue("channel_name")
	channel, err := schema.New(h.bot.DB).GetChannelByName(r.Context(), channelName)
	if err != nil {
		return nil, err
	}

	return channel, nil
}

func (h *httpHandlers) listMessages(r *http.Request) (any, error) {
	channelName := r.PathValue("channel_name")
	channel, err := schema.New(h.bot.DB).GetChannelByName(r.Context(), channelName)
	if err != nil {
		return nil, err
	}

	n := cmp.Or(r.URL.Query().Get("n"), "1000")
	nInt, err := strconv.ParseInt(n, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid n: %w", err)
	}

	return schema.New(h.bot.DB).GetAllMessages(r.Context(), schema.GetAllMessagesParams{
		ChannelID: channel.ID,
		N:         int32(nInt),
	})
}

func (h *httpHandlers) onboardChannel(r *http.Request) (any, error) {
	channelName := r.PathValue("channel_name")
	channel, err := schema.New(h.bot.DB).GetChannelByName(r.Context(), channelName)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	channelID := channel.ID
	if errors.Is(err, pgx.ErrNoRows) {
		channels, err := h.slackIntegration.GetBotChannels()
		if err != nil {
			return nil, err
		}

		for _, channel := range channels {
			if channel.Name == channelName {
				channelID = channel.ID
				break
			}
		}
	}

	if channelID == "" {
		return nil, fmt.Errorf("channel not found")
	}

	lastNMsgs := cmp.Or(r.URL.Query().Get("n"), "10")
	lastNMsgsInt, err := strconv.Atoi(lastNMsgs)
	if err != nil {
		return nil, fmt.Errorf("invalid last_n_msgs: %w", err)
	}

	tx, err := h.bot.DB.Begin(r.Context())
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(r.Context())

	onboarding, err := h.bot.EnsureChannel(r.Context(), tx, channelID)
	if err != nil {
		return nil, fmt.Errorf("ensuring channel: %w", err)
	}

	if !onboarding {
		if _, err := h.bot.RiverClient.InsertTx(r.Context(), tx, background.ChannelOnboardWorkerArgs{
			ChannelID: channelID,
			LastNMsgs: lastNMsgsInt,
		}, nil); err != nil {
			return nil, err
		}
	}

	return nil, tx.Commit(r.Context())
}

func (h *httpHandlers) agentMode(r *http.Request) (any, error) {
	channelName := r.PathValue("channel_name")
	channel, err := schema.New(h.bot.DB).GetChannelByName(r.Context(), channelName)
	if err != nil {
		return nil, err
	}

	enable := r.URL.Query().Get("enable")
	if enable == "true" {
		if err := h.bot.EnableAgentMode(r.Context(), channel.ID); err != nil {
			return nil, err
		}
	} else {
		if err := h.bot.DisableAgentMode(r.Context(), channel.ID); err != nil {
			return nil, err
		}
	}

	return nil, nil
}

func (h *httpHandlers) listServices(r *http.Request) (any, error) {
	services, err := schema.New(h.bot.DB).GetServices(r.Context())
	if err != nil {
		return nil, err
	}

	return services, nil
}

func (h *httpHandlers) listAlerts(r *http.Request) (any, error) {
	serviceName := r.PathValue("service")

	priorityFilter := r.URL.Query().Get("priority")
	alerts, err := schema.New(h.bot.DB).GetAlerts(r.Context(), serviceName)
	if err != nil {
		return nil, err
	}

	if priorityFilter != "" {
		filteredAlerts := make([]schema.GetAlertsRow, 0, len(alerts))
		for _, alert := range alerts {
			if alert.Priority == priorityFilter {
				filteredAlerts = append(filteredAlerts, alert)
			}
		}

		alerts = filteredAlerts
	}

	sort.Slice(alerts, func(i, j int) bool {
		return cmp.Or(
			alerts[i].Priority < alerts[j].Priority,
			alerts[i].Alert < alerts[j].Alert,
		)
	})

	return alerts, nil
}

func (h *httpHandlers) listThreadMessages(r *http.Request) (any, error) {
	serviceName := r.PathValue("service")
	alertName := r.PathValue("alert")

	qtx := schema.New(h.bot.DB)
	msgs, err := qtx.GetThreadMessagesByServiceAndAlert(r.Context(), schema.GetThreadMessagesByServiceAndAlertParams{
		Service: serviceName,
		Alert:   alertName,
		BotID:   h.slackIntegration.BotUserID(),
	})
	if err != nil {
		return nil, err
	}

	return msgs, nil
}

func (h *httpHandlers) getRunbook(r *http.Request) (any, error) {
	serviceName := r.PathValue("service")
	alertName := r.PathValue("alert")

	rbk, err := runbook.Get(r.Context(), schema.New(h.bot.DB), h.llmClient, serviceName, alertName, h.slackIntegration.BotUserID())
	if err != nil {
		return nil, err
	}

	return rbk, nil
}

func (h *httpHandlers) postRunbook(r *http.Request) (any, error) {
	serviceName := r.PathValue("service")
	alertName := r.PathValue("alert")

	channelID := r.URL.Query().Get("channel_id")

	qtx := schema.New(h.bot.DB)
	runbookMessage, err := runbook.Get(
		r.Context(),
		qtx,
		h.llmClient,
		serviceName,
		alertName,
		h.slackIntegration.BotUserID(),
	)
	if err != nil {
		return nil, fmt.Errorf("getting runbook: %w", err)
	}

	if runbookMessage == nil {
		return nil, fmt.Errorf("no runbook found")
	}

	blocks := runbook.Format(serviceName, alertName, runbookMessage)
	if err := h.slackIntegration.PostMessage(r.Context(), channelID, blocks...); err != nil {
		return nil, err
	}

	return nil, nil
}

func (h *httpHandlers) docsStatus(r *http.Request) (any, error) {
	status, err := schema.New(h.bot.DB).GetDocumentationStatus(r.Context())
	if err != nil {
		return nil, err
	}

	return status, nil
}

func (h *httpHandlers) postRefresh(r *http.Request) (any, error) {
	name := r.URL.Query().Get("name")
	if name == "" {
		return nil, fmt.Errorf("name parameter is required")
	}

	for _, source := range h.docsConfig.Sources {
		if source.Name == name {
			_, err := h.bot.RiverClient.Insert(r.Context(), background.DocumentationRefreshArgs{Source: source}, nil)
			if err != nil {
				return nil, fmt.Errorf("inserting documentation refresh: %w", err)
			}

			return nil, nil
		}
	}

	return nil, fmt.Errorf("source not found")
}

func (h *httpHandlers) generateCommand(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.Bool("force_trace", true))
	channelID := r.URL.Query().Get("channel_id")
	threadTS := r.URL.Query().Get("ts")
	if channelID == "" || threadTS == "" {
		http.Error(w, "channel_id and ts parameters are required", http.StatusBadRequest)
		return
	}

	msg, err := h.bot.GetMessage(ctx, channelID, threadTS)
	if err != nil {
		http.Error(w, fmt.Sprintf("getting message: %v", err), http.StatusInternalServerError)
		return
	}
	span.SetAttributes(attribute.String("user", msg.Attrs.Message.User), attribute.String("channel.id", msg.ChannelID), attribute.String("slack.ts", msg.Ts))

	response, err := h.commands.Generate(ctx, channelID, threadTS, msg.Attrs, true /* force */)
	if err != nil {
		http.Error(w, fmt.Sprintf("generating command: %v", err), http.StatusInternalServerError)
		return
	}
	span.SetAttributes(attribute.Int("response.message.size", len(response)))

	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(response))
}

func (h *httpHandlers) respondCommand(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.Bool("force_trace", true))
	channelID := r.URL.Query().Get("channel_id")
	threadTS := r.URL.Query().Get("ts")
	if channelID == "" || threadTS == "" {
		http.Error(w, "channel_id and ts parameters are required", http.StatusBadRequest)
		return
	}

	msg, err := h.bot.GetMessage(ctx, channelID, threadTS)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "message not found", http.StatusNotFound)
			return
		}

		http.Error(w, fmt.Sprintf("getting message: %v", err), http.StatusInternalServerError)
		return
	}
	span.SetAttributes(attribute.String("user", msg.Attrs.Message.User), attribute.String("channel.id", msg.ChannelID), attribute.String("slack.ts", msg.Ts))

	err = h.commands.Respond(ctx, channelID, threadTS, msg.Attrs, true /* force */)
	if err != nil {
		http.Error(w, fmt.Sprintf("generating command: %v", err), http.StatusInternalServerError)
		return
	}
}
