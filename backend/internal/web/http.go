package web

import (
	"context"
	"embed"
	"encoding/json"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/rs/cors"
	"riverqueue.com/riverui"

	"github.com/dynoinc/ratchet/internal/report"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
	"github.com/jackc/pgx/v5/pgtype"
)

//go:embed templates/* templates/components/*
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

type httpHandlers struct {
	dbQueries *schema.Queries
	templates *template.Template
}

type Config struct {
	CORSAllowedOrigins string
}

func New(ctx context.Context, db *pgxpool.Pool, riverClient *river.Client[pgx.Tx], config Config) (http.Handler, error) {
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
	mux.HandleFunc("GET /team/{team}/instant-report", handlers.instantReport)
	mux.HandleFunc("POST /team/{team}/instant-report", handlers.instantReport)

	// Add API endpoints for the frontend
	mux.HandleFunc("GET /api/channels", handlers.getChannels)
	mux.HandleFunc("GET /api/channels/{channelId}/reports", handlers.getChannelReports)
	mux.HandleFunc("GET /api/channels/{channelId}/instant-report", handlers.generateInstantReport)
	mux.HandleFunc("GET /api/health", handlers.healthCheck)

	// Create CORS middleware
	allowedOrigins := strings.Split(config.CORSAllowedOrigins, ",")
	slog.Info("Configuring CORS with allowed origins", "origins", allowedOrigins)

	corsMiddleware := cors.New(cors.Options{
		AllowedOrigins:   allowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	})

	// Wrap the mux with CORS middleware
	return corsMiddleware.Handler(mux), nil
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

// API handlers for the frontend
func (h *httpHandlers) getChannels(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	channels, err := h.dbQueries.GetChannels(r.Context())
	if err != nil {
		slog.Error("Error fetching channels", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Filter and transform channels to match frontend expectations
	var response []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}

	for _, ch := range channels {
		if ch.Attrs.Name != "" {
			response = append(response, struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}{
				ID:   ch.ChannelID,
				Name: ch.Attrs.Name,
			})
		}
	}

	slog.Info("Sending channels response", "count", len(response))
	slog.Info("Response data", "channels", response)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		slog.Error("Error encoding channels response", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (h *httpHandlers) getChannelReports(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Extract channelId from the URL path
	channelID := r.PathValue("channelId")
	if channelID == "" {
		http.Error(w, "Channel ID is required", http.StatusBadRequest)
		return
	}

	reports, err := h.dbQueries.GetChannelReports(r.Context(), schema.GetChannelReportsParams{
		ChannelID: channelID,
		Limit:     50,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Transform reports to match frontend expectations
	var response []struct {
		ID        string    `json:"id"`
		ChannelID string    `json:"channelId"`
		Content   string    `json:"content"`
		CreatedAt time.Time `json:"createdAt"`
	}

	for _, report := range reports {
		r, err := h.dbQueries.GetReport(r.Context(), report.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		c, err := json.Marshal(r.ReportData)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		response = append(response, struct {
			ID        string    `json:"id"`
			ChannelID string    `json:"channelId"`
			Content   string    `json:"content"`
			CreatedAt time.Time `json:"createdAt"`
		}{
			ID:        strconv.Itoa(int(report.ID)),
			ChannelID: channelID,
			Content:   string(c),
			CreatedAt: r.CreatedAt.Time,
		})
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (h *httpHandlers) healthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (h *httpHandlers) generateInstantReport(w http.ResponseWriter, r *http.Request) {
	slog.Info("Instant report request received",
		"method", r.Method,
		"path", r.URL.Path,
		"channelId", r.PathValue("channelId"))

	w.Header().Set("Content-Type", "application/json")

	channelID := r.PathValue("channelId")
	if channelID == "" {
		http.Error(w, "Channel ID is required", http.StatusBadRequest)
		return
	}

	// Get channel by ID
	channel, err := h.dbQueries.GetChannel(r.Context(), channelID)
	if err != nil {
		http.Error(w, "Channel not found", http.StatusNotFound)
		return
	}

	// Calculate the time range for the report
	endDate := time.Now()
	startDate := endDate.AddDate(0, 0, -7) // Last 7 days

	// Get incident statistics from database
	incidentStats, err := h.dbQueries.GetIncidentStatsByPeriod(r.Context(), schema.GetIncidentStatsByPeriodParams{
		ChannelID: channel.ChannelID,
		StartTimestamp: pgtype.Timestamptz{
			Time:  startDate,
			Valid: true,
		},
		StartTimestamp_2: pgtype.Timestamptz{
			Time:  endDate,
			Valid: true,
		},
	})
	if err != nil {
		slog.Error("Failed to get incident stats", "error", err)
		http.Error(w, "Failed to get incident stats", http.StatusInternalServerError)
		return
	}

	// Get top alerts from database
	topAlerts, err := h.dbQueries.GetTopAlerts(r.Context(), schema.GetTopAlertsParams{
		ChannelID: channel.ChannelID,
		StartTimestamp: pgtype.Timestamptz{
			Time:  startDate,
			Valid: true,
		},
		StartTimestamp_2: pgtype.Timestamptz{
			Time:  endDate,
			Valid: true,
		},
	})
	if err != nil {
		slog.Error("Failed to get top alerts", "error", err)
		http.Error(w, "Failed to get top alerts", http.StatusInternalServerError)
		return
	}

	// Create report generator
	generator, err := report.NewGenerator()
	if err != nil {
		slog.Error("Failed to create report generator", "error", err)
		http.Error(w, "Failed to create report generator", http.StatusInternalServerError)
		return
	}

	// Generate the report using the database data
	reportData, err := generator.GenerateReportData(channel.Attrs.Name, startDate, incidentStats, topAlerts)
	if err != nil {
		slog.Error("Failed to generate report", "error", err)
		http.Error(w, "Failed to generate report", http.StatusInternalServerError)
		return
	}

	webReport := generator.FormatForWeb(reportData)
	response := struct {
		ChannelID      string         `json:"channelId"`
		ChannelName    string         `json:"channelName"`
		WeekRange      string         `json:"weekRange"`
		CreatedAt      time.Time      `json:"createdAt"`
		Incidents      []dto.Incident `json:"incidents"`
		TopAlerts      []dto.Alert    `json:"topAlerts"`
		MitigationTime string         `json:"mitigationTime"`
	}{
		ChannelID:      channel.ChannelID,
		ChannelName:    channel.Attrs.Name,
		WeekRange:      formatDateRange(startDate, endDate),
		CreatedAt:      time.Now(),
		Incidents:      webReport.Incidents,
		TopAlerts:      webReport.TopAlerts,
		MitigationTime: webReport.MitigationTime,
	}

	slog.Info("Sending instant report response",
		"channelId", channel.ChannelID,
		"channelName", channel.Attrs.Name)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		slog.Error("Failed to encode response", "error", err)
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// getEnvOrDefault returns the value of an environment variable or a default value if not set
func getEnvOrDefault(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists && value != "" {
		return value
	}
	return defaultValue
}
