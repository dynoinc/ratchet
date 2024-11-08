package web

import (
	"context"
	"embed"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"riverqueue.com/riverui"

	"github.com/dynoinc/ratchet/internal/storage/schema"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

type httpHandlers struct {
	dbQueries *schema.Queries
	templates *template.Template
}

func New(ctx context.Context, db *pgxpool.Pool, riverClient *river.Client[pgx.Tx]) (http.Handler, error) {
	templates, err := template.ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}

	staticFS, err := fs.Sub(staticFS, "static")
	if err != nil {
		return nil, err
	}

	handlers := &httpHandlers{
		dbQueries: schema.New(db),
		templates: templates,
	}

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

	mux := http.NewServeMux()
	mux.Handle("/riverui/", riverServer)
	mux.Handle("GET /static/", http.StripPrefix("/static", http.FileServerFS(staticFS)))
	mux.HandleFunc("GET /{$}", handlers.root)
	mux.HandleFunc("GET /team/{team}", handlers.team)

	return mux, nil
}

func (h *httpHandlers) root(writer http.ResponseWriter, request *http.Request) {
	channels, err := h.dbQueries.GetSlackChannels(request.Context())
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}

	data := struct {
		Channels []schema.Channel
	}{
		Channels: channels,
	}

	writer.Header().Set("Content-Type", "text/html")
	err = h.templates.ExecuteTemplate(writer, "root.html", data)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (h *httpHandlers) team(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Content-Type", "text/html")
	err := h.templates.ExecuteTemplate(writer, "team.html", nil)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}
}
