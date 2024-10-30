package web

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rajatgoel/ratchet/internal/storage/schema"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

type httpHandlers struct {
	dbQueries *schema.Queries
	templates *template.Template
}

func New(db *pgxpool.Pool) (http.Handler, error) {
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

	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static", http.FileServerFS(staticFS)))
	mux.HandleFunc("GET /{$}", handlers.root)
	mux.HandleFunc("GET /{team}", handlers.team)
	return mux, nil
}

func (h *httpHandlers) root(writer http.ResponseWriter, request *http.Request) {
	teams, err := h.dbQueries.GetUniqueTeamNames(request.Context())
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}

	data := struct {
		Teams []string
	}{
		Teams: teams,
	}

	writer.Header().Set("Content-Type", "text/html")
	err = h.templates.ExecuteTemplate(writer, "root.html", data)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (h *httpHandlers) team(writer http.ResponseWriter, request *http.Request) {
	channels, err := h.dbQueries.GetSlackChannelsByTeamName(request.Context(), request.PathValue("team"))
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}

	data := struct {
		Channels []schema.SlackChannel
	}{
		Channels: channels,
	}
	writer.Header().Set("Content-Type", "text/html")
	err = h.templates.ExecuteTemplate(writer, "team.html", data)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}
}
