package web

import (
	"context"
	"encoding/json"
	"errors"
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
	apiMux.HandleFunc("GET /channels/{channel_name}/messages", handleJSON(handlers.listMessages))
	apiMux.HandleFunc("GET /channels/{channel_name}/onboard", handleJSON(handlers.onboardChannel))

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
	if err != nil {
		return nil, err
	}

	// submit job to river to onboard channel
	if _, err := h.riverClient.Insert(r.Context(), background.ChannelOnboardWorkerArgs{
		ChannelID: channel.ID,
	}, nil); err != nil {
		return nil, err
	}

	return nil, nil
}
