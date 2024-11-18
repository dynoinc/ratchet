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

//go:embed templates/* templates/components/*
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

type httpHandlers struct {
	dbQueries *schema.Queries
	templates *template.Template
}

func New(ctx context.Context, db *pgxpool.Pool, riverClient *river.Client[pgx.Tx]) (http.Handler, error) {
	templates, err := template.New("").
		Funcs(templateFuncs).
		ParseFS(templateFS, "templates/*.html", "templates/components/*.html")
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
	mux.HandleFunc("GET /team/{team}/report/{report}", handlers.reportDetail)

	return mux, nil
}

func (h *httpHandlers) root(writer http.ResponseWriter, request *http.Request) {
	channels, err := h.dbQueries.GetChannels(request.Context())
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}

	var channelsWithNames []schema.Channel
	for _, ch := range channels {
		if ch.Attrs.Name != "" {
			channelsWithNames = append(channelsWithNames, ch)
		}
	}

	data := struct {
		Channels []schema.Channel
	}{
		Channels: channelsWithNames,
	}

	writer.Header().Set("Content-Type", "text/html")
	err = h.templates.ExecuteTemplate(writer, "root.html", data)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}
}
