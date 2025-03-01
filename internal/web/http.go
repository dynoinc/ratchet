package web

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/earthboundkid/versioninfo/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/olekukonko/tablewriter"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/riverqueue/river"
	"riverqueue.com/riverui"

	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/llm"
	"github.com/dynoinc/ratchet/internal/modules/channel_monitor"
	"github.com/dynoinc/ratchet/internal/modules/recent_activity"
	"github.com/dynoinc/ratchet/internal/modules/report"
	"github.com/dynoinc/ratchet/internal/modules/runbook"
	"github.com/dynoinc/ratchet/internal/modules/usage"
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
	db               *pgxpool.Pool
	riverClient      *river.Client[pgx.Tx]
	slackIntegration slack_integration.Integration
	llmClient        llm.Client
}

func New(
	ctx context.Context,
	db *pgxpool.Pool,
	riverClient *river.Client[pgx.Tx],
	slackIntegration slack_integration.Integration,
	llmClient llm.Client,
) (http.Handler, error) {
	handlers := &httpHandlers{
		db:               db,
		riverClient:      riverClient,
		slackIntegration: slackIntegration,
		llmClient:        llmClient,
	}

	// River UI
	opts := &riverui.ServerOpts{
		Client: riverClient,
		DB:     db,
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
	apiMux.HandleFunc("GET /channels/{channel_name}/messages", handleJSON(handlers.listMessages))
	apiMux.HandleFunc("GET /channels/{channel_name}/report", handleJSON(handlers.generateReport))
	apiMux.HandleFunc("POST /channels/{channel_name}/onboard", handleJSON(handlers.onboardChannel))

	// Services
	apiMux.HandleFunc("GET /services", handleJSON(handlers.listServices))
	apiMux.HandleFunc("GET /services/{service}/alerts", handleJSON(handlers.listAlerts))
	apiMux.HandleFunc("GET /services/{service}/alerts/{alert}/messages", handleJSON(handlers.listThreadMessages))
	apiMux.HandleFunc("GET /services/{service}/alerts/{alert}/runbook", handleJSON(handlers.getRunbook))
	apiMux.HandleFunc("GET /services/{service}/alerts/{alert}/recent-activity", handleJSON(handlers.getRecentActivity))
	apiMux.HandleFunc("POST /services/{service}/alerts/{alert}/post-runbook", handleJSON(handlers.postRunbook))

	// Bot
	apiMux.HandleFunc("GET /bot/search", handlers.search)
	apiMux.HandleFunc("POST /bot/usage", handleJSON(handlers.postUsage))

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
	channels, err := schema.New(h.db).GetAllChannels(r.Context())
	if err != nil {
		return nil, err
	}

	sort.Slice(channels, func(i, j int) bool {
		return channels[i].Attrs.Name < channels[j].Attrs.Name
	})

	return channels, nil
}

func (h *httpHandlers) listMessages(r *http.Request) (any, error) {
	channelName := r.PathValue("channel_name")
	channel, err := schema.New(h.db).GetChannelByName(r.Context(), channelName)
	if err != nil {
		return nil, err
	}

	n := cmp.Or(r.URL.Query().Get("n"), "1000")
	nInt, err := strconv.ParseInt(n, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid n: %w", err)
	}

	return schema.New(h.db).GetAllMessages(r.Context(), schema.GetAllMessagesParams{
		ChannelID: channel.ID,
		N:         int32(nInt),
	})
}

func (h *httpHandlers) onboardChannel(r *http.Request) (any, error) {
	channelName := r.PathValue("channel_name")
	channel, err := schema.New(h.db).GetChannelByName(r.Context(), channelName)
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

	lastNMsgs := cmp.Or(r.URL.Query().Get("n"), "1000")
	lastNMsgsInt, err := strconv.Atoi(lastNMsgs)
	if err != nil {
		return nil, fmt.Errorf("invalid last_n_msgs: %w", err)
	}

	// submit job to river to onboard channel
	if _, err := h.riverClient.Insert(r.Context(), background.ChannelOnboardWorkerArgs{
		ChannelID: channelID,
		LastNMsgs: lastNMsgsInt,
	}, nil); err != nil {
		return nil, err
	}

	return nil, nil
}

func (h *httpHandlers) generateReport(r *http.Request) (any, error) {
	channelName := r.PathValue("channel_name")
	channel, err := schema.New(h.db).GetChannelByName(r.Context(), channelName)
	if err != nil {
		return nil, err
	}

	if err := report.Post(r.Context(), schema.New(h.db), h.llmClient, h.slackIntegration, channel.ID); err != nil {
		return nil, err
	}

	return nil, nil
}

func (h *httpHandlers) listServices(r *http.Request) (any, error) {
	services, err := schema.New(h.db).GetServices(r.Context())
	if err != nil {
		return nil, err
	}

	return services, nil
}

func (h *httpHandlers) listAlerts(r *http.Request) (any, error) {
	serviceName := r.PathValue("service")

	priorityFilter := r.URL.Query().Get("priority")
	alerts, err := schema.New(h.db).GetAlerts(r.Context(), serviceName)
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

	qtx := schema.New(h.db)
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

	runbook, err := runbook.Get(r.Context(), schema.New(h.db), h.llmClient, serviceName, alertName, h.slackIntegration.BotUserID())
	if err != nil {
		return nil, err
	}

	return runbook, nil
}

func (h *httpHandlers) getRecentActivity(r *http.Request) (any, error) {
	serviceName := r.PathValue("service")
	alertName := r.PathValue("alert")

	interval := cmp.Or(r.URL.Query().Get("interval"), "1h")
	intervalDuration, err := time.ParseDuration(interval)
	if err != nil {
		return nil, err
	}

	runbook, err := runbook.Get(r.Context(), schema.New(h.db), h.llmClient, serviceName, alertName, h.slackIntegration.BotUserID())
	if err != nil {
		return nil, err
	}

	messages, err := recent_activity.Get(
		r.Context(),
		schema.New(h.db),
		h.llmClient,
		runbook.SearchQuery,
		intervalDuration,
		h.slackIntegration.BotUserID(),
	)
	if err != nil {
		return nil, err
	}

	return messages, nil
}

func (h *httpHandlers) postRunbook(r *http.Request) (any, error) {
	serviceName := r.PathValue("service")
	alertName := r.PathValue("alert")

	channelID := r.URL.Query().Get("channel_id")
	interval := cmp.Or(r.URL.Query().Get("interval"), "1h")
	intervalDuration, err := time.ParseDuration(interval)
	if err != nil {
		return nil, err
	}

	qtx := schema.New(h.db)
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

	updates, err := recent_activity.Get(
		r.Context(),
		qtx,
		h.llmClient,
		runbookMessage.SearchQuery,
		intervalDuration,
		h.slackIntegration.BotUserID(),
	)
	if err != nil {
		return nil, fmt.Errorf("getting updates: %w", err)
	}

	blocks := runbook.Format(serviceName, alertName, runbookMessage, updates)
	if err := h.slackIntegration.PostMessage(r.Context(), channelID, blocks...); err != nil {
		return nil, err
	}

	return nil, nil
}

func (h *httpHandlers) search(w http.ResponseWriter, r *http.Request) {
	var query string
	serviceName := r.URL.Query().Get("service")
	alertName := r.URL.Query().Get("alert")

	if serviceName != "" && alertName != "" {
		qtx := schema.New(h.db)
		runbookMessage, err := runbook.Get(
			r.Context(),
			qtx,
			h.llmClient,
			serviceName,
			alertName,
			h.slackIntegration.BotUserID(),
		)
		if err != nil {
			http.Error(w, fmt.Sprintf("getting runbook: %v", err), http.StatusInternalServerError)
			return
		}
		if runbookMessage == nil {
			http.Error(w, "no runbook found", http.StatusNotFound)
			return
		}
		query = runbookMessage.SearchQuery
	} else {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, fmt.Sprintf("reading request body: %v", err), http.StatusInternalServerError)
			return
		}
		query = string(body)
	}

	interval := cmp.Or(r.URL.Query().Get("interval"), "1h")
	intervalDuration, err := time.ParseDuration(interval)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid interval: %v", err), http.StatusBadRequest)
		return
	}

	n := cmp.Or(r.URL.Query().Get("n"), "10")
	nInt, err := strconv.Atoi(n)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid n: %v", err), http.StatusBadRequest)
		return
	}

	updates, err := recent_activity.GetDebug(
		r.Context(),
		schema.New(h.db),
		h.llmClient,
		query,
		intervalDuration,
		h.slackIntegration.BotUserID(),
	)
	if err != nil {
		http.Error(w, fmt.Sprintf("getting updates: %v", err), http.StatusInternalServerError)
		return
	}

	table := tablewriter.NewWriter(w)
	table.SetHeader([]string{"Link", "Text", "Text Tokens", "Query Tokens", "Lexical Score", "Lexical Rank", "Semantic Score", "Semantic Rank", "RRF Score"})
	table.SetBorder(true)
	table.SetRowLine(true)
	table.SetAutoWrapText(false)
	table.SetColWidth(120)

	limit := nInt
	if limit > len(updates) {
		limit = len(updates)
	}

	for _, update := range updates[:limit] {
		text := update.MessageText
		if len(text) > 100 {
			text = text[:97] + "..."
		}
		messageLink := fmt.Sprintf("https://slack.com/archives/%s/p%s", update.ChannelID, strings.ReplaceAll(update.Ts, ".", ""))

		// Get text tokens
		textTokens := ""
		if update.TextTokens != "" {
			textTokens = update.TextTokens
		}
		if len(textTokens) > 100 {
			textTokens = textTokens[:97] + "..."
		}

		// Get query tokens
		queryTokens := ""
		if update.QueryTokens != "" {
			queryTokens = update.QueryTokens
		}
		if len(queryTokens) > 100 {
			queryTokens = queryTokens[:97] + "..."
		}

		table.Append([]string{
			messageLink,
			text,
			textTokens,
			queryTokens,
			fmt.Sprintf("%.4f", update.LexicalScore),
			fmt.Sprintf("%d", update.LexicalRank),
			fmt.Sprintf("%.4f", update.SemanticDistance),
			fmt.Sprintf("%d", update.SemanticRank),
			fmt.Sprintf("%.4f", update.RrfScore),
		})
	}

	table.Render()
}

func (h *httpHandlers) postUsage(r *http.Request) (any, error) {
	channelID := r.URL.Query().Get("channel_id")

	if err := usage.Post(r.Context(), schema.New(h.db), h.llmClient, h.slackIntegration, channelID); err != nil {
		return nil, err
	}

	return nil, nil
}
