package web

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"

	"github.com/carlmjohnson/versioninfo"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/riverqueue/river"
	"riverqueue.com/riverui"

	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/storage/schema"
)

type httpHandlers struct {
	db          *pgxpool.Pool
	riverClient *river.Client[pgx.Tx]
}

func handleJSON(handler func(*http.Request) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		result, err := handler(r)
		if err != nil {
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

func New(
	ctx context.Context,
	db *pgxpool.Pool,
	riverClient *river.Client[pgx.Tx],
) (http.Handler, error) {
	handlers := &httpHandlers{
		db:          db,
		riverClient: riverClient,
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
		return nil, fmt.Errorf("failed to create riverui server: %w", err)
	}
	if err := riverServer.Start(ctx); err != nil {
		return nil, fmt.Errorf("error starting riverui server: %w", err)
	}

	// API
	apiMux := http.NewServeMux()
	apiMux.HandleFunc("GET /channels", handleJSON(handlers.listChannels))
	apiMux.HandleFunc("GET /channels/{channelID}/messages", handleJSON(handlers.listMessages))
	apiMux.HandleFunc("GET /channels/{channelID}/incidents", handleJSON(handlers.listIncidents))
	apiMux.HandleFunc("POST /channels/{channelID}/refresh_channel_info", handleJSON(handlers.refreshChannelInfo))
	apiMux.HandleFunc("POST /channels/{channelID}/reingest_messages", handleJSON(handlers.reingestMessages))
	apiMux.HandleFunc("POST /channels/{channelID}/reclassify_messages", handleJSON(handlers.reclassifyMessages))
	apiMux.HandleFunc("POST /channels/{channelID}/post_report", handleJSON(handlers.postReport))

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
	channels, err := schema.New(h.db).GetChannels(r.Context())
	if err != nil {
		return nil, err
	}

	sort.Slice(channels, func(i, j int) bool {
		return channels[i].Attrs.Name < channels[j].Attrs.Name
	})

	return channels, nil
}

func (h *httpHandlers) listMessages(r *http.Request) (any, error) {
	channelID := r.PathValue("channelID")
	return schema.New(h.db).GetAllMessages(r.Context(), channelID)
}

func (h *httpHandlers) listIncidents(r *http.Request) (any, error) {
	channelID := r.PathValue("channelID")
	return schema.New(h.db).GetAllIncidents(r.Context(), channelID)
}

func (h *httpHandlers) refreshChannelInfo(r *http.Request) (any, error) {
	channelID := r.PathValue("channelID")
	_, err := h.riverClient.Insert(r.Context(), background.ChannelInfoWorkerArgs{
		ChannelID: channelID,
	}, nil)

	return nil, err
}

func (h *httpHandlers) reingestMessages(r *http.Request) (any, error) {
	channelID := r.PathValue("channelID")

	ctx := r.Context()
	tx, err := h.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := schema.New(h.db).WithTx(tx)
	if err := qtx.UpdateChannelSlackTSWatermark(ctx, schema.UpdateChannelSlackTSWatermarkParams{
		ChannelID: channelID,
	}); err != nil {
		return nil, err
	}

	if _, err := h.riverClient.InsertTx(r.Context(), tx, background.MessagesIngestionWorkerArgs{
		ChannelID: channelID,
	}, nil); err != nil {
		return nil, err
	}

	return nil, tx.Commit(ctx)
}

func (h *httpHandlers) reclassifyMessages(r *http.Request) (any, error) {
	channelID := r.PathValue("channelID")

	messages, err := schema.New(h.db).GetAllMessages(r.Context(), channelID)
	if err != nil {
		return nil, err
	}

	var jobs []river.InsertManyParams
	for _, message := range messages {
		jobs = append(jobs, river.InsertManyParams{
			Args: background.ClassifierArgs{
				ChannelID: channelID,
				SlackTS:   message.SlackTs,
			},
		})
	}

	if len(jobs) > 0 {
		if _, err := h.riverClient.InsertManyFast(r.Context(), jobs); err != nil {
			return nil, err
		}
	}

	return nil, nil
}

func (h *httpHandlers) postReport(r *http.Request) (any, error) {
	channelID := r.PathValue("channelID")

	_, err := h.riverClient.Insert(r.Context(), background.WeeklyReportJobArgs{
		ChannelID: channelID,
	}, nil)

	return nil, err
}
