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
	"time"

	"github.com/carlmjohnson/versioninfo"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/riverqueue/river"
	"riverqueue.com/riverui"

	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/background/runbook_worker"
	"github.com/dynoinc/ratchet/internal/llm"
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
	slackIntegration *slack_integration.Integration
	llmClient        *llm.Client
}

func New(
	ctx context.Context,
	db *pgxpool.Pool,
	riverClient *river.Client[pgx.Tx],
	slackIntegration *slack_integration.Integration,
	llmClient *llm.Client,
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

	apiMux := http.NewServeMux()

	// Channels
	apiMux.HandleFunc("GET /channels", handleJSON(handlers.listChannels))
	apiMux.HandleFunc("GET /channels/{channel_name}/messages", handleJSON(handlers.listMessages))
	apiMux.HandleFunc("GET /channels/{channel_name}/report", handleJSON(handlers.generateReport))
	apiMux.HandleFunc("POST /channels/{channel_name}/onboard", handleJSON(handlers.onboardChannel))

	// Services
	apiMux.HandleFunc("GET /services", handleJSON(handlers.listServices))
	apiMux.HandleFunc("GET /services/{service}/alerts", handleJSON(handlers.listAlerts))
	apiMux.HandleFunc("GET /services/{service}/alerts/{alert}/runbook", handleJSON(handlers.getRunbook))
	apiMux.HandleFunc("GET /services/{service}/alerts/{alert}/updates", handleJSON(handlers.getUpdates))
	apiMux.HandleFunc("POST /services/{service}/refresh-runbooks", handleJSON(handlers.refreshRunbooks))
	apiMux.HandleFunc("POST /services/{service}/alerts/{alert}/refresh-runbook", handleJSON(handlers.refreshRunbook))

	mux := http.NewServeMux()
	mux.Handle("/riverui/", riverServer)
	mux.Handle("/api/", http.StripPrefix("/api", apiMux))
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

	return schema.New(h.db).GetAllMessages(r.Context(), channel.ID)
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

	// submit job to river to onboard channel
	if _, err := h.riverClient.Insert(r.Context(), background.ChannelOnboardWorkerArgs{
		ChannelID: channelID,
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

	if _, err := h.riverClient.Insert(r.Context(), background.ReportWorkerArgs{
		ChannelID: channel.ID,
	}, nil); err != nil {
		return nil, err
	}

	return nil, nil
}

func (h *httpHandlers) refreshRunbook(r *http.Request) (any, error) {
	serviceName := r.PathValue("service")
	alertName := r.PathValue("alert")

	forceRecreate := r.URL.Query().Get("force_recreate") == "1"
	if _, err := h.riverClient.Insert(r.Context(), background.UpdateRunbookWorkerArgs{
		Service:       serviceName,
		Alert:         alertName,
		ForceRecreate: forceRecreate,
	}, nil); err != nil {
		return nil, err
	}

	return nil, nil
}

func (h *httpHandlers) refreshRunbooks(r *http.Request) (any, error) {
	serviceName := r.PathValue("service")
	forceRecreate := r.URL.Query().Get("force_recreate") == "1"

	alerts, err := schema.New(h.db).GetAlerts(r.Context(), serviceName)
	if err != nil {
		return nil, err
	}

	if len(alerts) == 0 {
		return nil, nil
	}

	var params []river.InsertManyParams
	for _, alert := range alerts {
		params = append(params, river.InsertManyParams{
			Args: background.UpdateRunbookWorkerArgs{
				Service:       alert.Service,
				Alert:         alert.Alert,
				ForceRecreate: forceRecreate,
			},
		})
	}

	tx, err := h.db.Begin(r.Context())
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(r.Context())

	if _, err := h.riverClient.InsertManyTx(r.Context(), tx, params); err != nil {
		return nil, err
	}

	return nil, tx.Commit(r.Context())
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

	type alertWithRunbook struct {
		schema.GetAlertsRow
		Runbook string
	}

	alertsWithRunbooks := make([]alertWithRunbook, len(alerts))
	for i, alert := range alerts {
		runbook, err := schema.New(h.db).GetRunbook(r.Context(), schema.GetRunbookParams{
			ServiceName: alert.Service,
			AlertName:   alert.Alert,
		})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}

		alertsWithRunbooks[i] = alertWithRunbook{
			GetAlertsRow: alert,
			Runbook:      runbook.Attrs.Runbook,
		}
	}

	return alertsWithRunbooks, nil
}

func (h *httpHandlers) getRunbook(r *http.Request) (any, error) {
	serviceName := r.PathValue("service")
	alertName := r.PathValue("alert")

	runbook, err := schema.New(h.db).GetRunbook(r.Context(), schema.GetRunbookParams{
		ServiceName: serviceName,
		AlertName:   alertName,
	})
	if err != nil {
		return nil, err
	}

	return runbook, nil
}

func (h *httpHandlers) getUpdates(r *http.Request) (any, error) {
	serviceName := r.PathValue("service")
	alertName := r.PathValue("alert")

	interval := r.URL.Query().Get("interval")
	if interval == "" {
		interval = "1h"
	}

	intervalDuration, err := time.ParseDuration(interval)
	if err != nil {
		return nil, err
	}

	updates, err := runbook_worker.GetUpdates(r.Context(), h.db, h.llmClient, serviceName, alertName, intervalDuration)
	if err != nil {
		return nil, err
	}

	// remove embedding from updates, they are huge
	for i := range updates {
		updates[i].Embedding = nil
	}

	return updates, nil
}
