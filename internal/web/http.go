package web

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

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
	apiMux.HandleFunc("GET /channels", handlers.listChannels)
	apiMux.HandleFunc("GET /channels/{channelID}/messages", handlers.listMessages)
	apiMux.HandleFunc("GET /channels/{channelID}/incidents", handlers.listIncidents)
	apiMux.HandleFunc("POST /channels/{channelID}/refresh_channel_info", handlers.refreshChannelInfo)
	apiMux.HandleFunc("POST /channels/{channelID}/reingest_messages", handlers.reingestMessages)
	apiMux.HandleFunc("POST /channels/{channelID}/reclassify_messages", handlers.reclassifyMessages)
	apiMux.HandleFunc("POST /channels/{channelID}/post_report", handlers.postReport)

	mux := http.NewServeMux()
	mux.Handle("/riverui/", riverServer)
	mux.Handle("/metrics", promhttp.Handler())
	mux.Handle("/api/", http.StripPrefix("/api", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		apiMux.ServeHTTP(w, r)
	})))
	return mux, nil
}

func (h *httpHandlers) listChannels(writer http.ResponseWriter, request *http.Request) {
	channels, err := schema.New(h.db).GetChannels(request.Context())
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := json.NewEncoder(writer).Encode(channels); err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (h *httpHandlers) listMessages(writer http.ResponseWriter, request *http.Request) {
	channelID := request.PathValue("channelID")

	messages, err := schema.New(h.db).GetAllMessages(request.Context(), channelID)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := json.NewEncoder(writer).Encode(messages); err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (h *httpHandlers) listIncidents(writer http.ResponseWriter, request *http.Request) {
	channelID := request.PathValue("channelID")

	incidents, err := schema.New(h.db).GetAllIncidents(request.Context(), channelID)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := json.NewEncoder(writer).Encode(incidents); err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (h *httpHandlers) refreshChannelInfo(writer http.ResponseWriter, request *http.Request) {
	channelID := request.PathValue("channelID")

	_, err := h.riverClient.Insert(request.Context(), background.ChannelInfoWorkerArgs{
		ChannelID: channelID,
	}, nil)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
	}

	_ = json.NewEncoder(writer).Encode(map[string]interface{}{})
}

func (h *httpHandlers) reingestMessages(writer http.ResponseWriter, request *http.Request) {
	channelID := request.PathValue("channelID")

	_, err := h.riverClient.Insert(request.Context(), background.MessagesIngestionWorkerArgs{
		ChannelID: channelID,
	}, &river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true}})
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
	}

	_ = json.NewEncoder(writer).Encode(map[string]interface{}{})
}

func (h *httpHandlers) reclassifyMessages(writer http.ResponseWriter, request *http.Request) {
	channelID := request.PathValue("channelID")

	messages, err := schema.New(h.db).GetAllMessages(request.Context(), channelID)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
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
		if _, err := h.riverClient.InsertManyFast(request.Context(), jobs); err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	_ = json.NewEncoder(writer).Encode(map[string]interface{}{})
}

func (h *httpHandlers) postReport(writer http.ResponseWriter, request *http.Request) {
	channelID := request.PathValue("channelID")

	_, err := h.riverClient.Insert(request.Context(), background.WeeklyReportJobArgs{
		ChannelID: channelID,
	}, nil)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
	}

	_ = json.NewEncoder(writer).Encode(map[string]interface{}{})
}
