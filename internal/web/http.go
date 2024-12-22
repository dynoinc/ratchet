package web

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"riverqueue.com/riverui"

	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/storage/schema"
)

type httpHandlers struct {
	db          *pgxpool.Pool
	riverClient *river.Client[pgx.Tx]
}

func New(ctx context.Context, db *pgxpool.Pool, riverClient *river.Client[pgx.Tx]) (http.Handler, error) {
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
		return nil, err
	}
	if err := riverServer.Start(ctx); err != nil {
		return nil, err
	}

	// API
	apiMux := http.NewServeMux()
	apiMux.HandleFunc("GET /channels", handlers.listChannels)
	apiMux.HandleFunc("POST /channels/{channelID}/refresh_channel_info", handlers.refreshChannelInfo)
	apiMux.HandleFunc("POST /channels/{channelID}/reingest_messages", handlers.reingestMessages)
	apiMux.HandleFunc("GET /channels/{channelID}/messages", handlers.listMessages)
	apiMux.HandleFunc("GET /channels/{channelID}/incidents", handlers.listIncidents)

	mux := http.NewServeMux()
	mux.Handle("/riverui/", riverServer)
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

func (h *httpHandlers) refreshChannelInfo(writer http.ResponseWriter, request *http.Request) {
	channelID := request.PathValue("channelID")

	_, err := h.riverClient.Insert(request.Context(), background.ChannelInfoWorkerArgs{
		ChannelID: channelID,
	}, nil)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
	}

	json.NewEncoder(writer).Encode(map[string]interface{}{})
}

func (h *httpHandlers) reingestMessages(writer http.ResponseWriter, request *http.Request) {
	channelID := request.PathValue("channelID")

	_, err := h.riverClient.Insert(request.Context(), background.MessagesIngestionWorkerArgs{
		ChannelID: channelID,
	}, &river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true}})
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
	}

	json.NewEncoder(writer).Encode(map[string]interface{}{})
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
