package internal

import (
	"embed"
	"html/template"
	"net/http"

	"github.com/rajatgoel/ratchet/internal/schema"
)

//go:embed templates/*.html static/*.css
var templateFS embed.FS

type httpHandlers struct {
	dbQueries *schema.Queries
	templates *template.Template
}

func NewHandler(dbQueries *schema.Queries) http.Handler {
	templates := template.Must(template.ParseFS(templateFS, "templates/*.html"))

	handlers := &httpHandlers{
		dbQueries: dbQueries,
		templates: templates,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", handlers.root)
	mux.HandleFunc("GET /{team}", handlers.team)
	mux.HandleFunc("/static/stylesheet.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		css, _ := templateFS.ReadFile("static/stylesheet.css")
		w.Write(css)
	})
	return mux
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
