package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"syscall"
	"time"

	"github.com/carlmjohnson/versioninfo"
	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"
	"github.com/lmittmann/tint"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/riverqueue/river"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/sdk/metric"
	"golang.org/x/sync/errgroup"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/background/channel_onboard_worker"
	"github.com/dynoinc/ratchet/internal/background/classifier_worker"
	"github.com/dynoinc/ratchet/internal/slack_integration"
	"github.com/dynoinc/ratchet/internal/storage"
	"github.com/dynoinc/ratchet/internal/web"
)

type openAIConfig struct {
	APIKey string `envconfig:"API_KEY"`
	URL    string `default:"https://api.openai.com/v1/"`
	Model  string `default:"o1-mini"`
}

type config struct {
	DevMode bool `split_words:"true" default:"true"`

	// Database configuration
	Database storage.DatabaseConfig

	// Classifier configuration
	Classifier classifier_worker.Config

	// OpenAI configuration
	OpenAI openAIConfig `envconfig:"OPENAI"`

	// Slack configuration
	SlackBotToken   string `split_words:"true" required:"true"`
	SlackAppToken   string `split_words:"true" required:"true"`
	SlackDevChannel string `split_words:"true"`

	// HTTP configuration
	HTTPAddr string `split_words:"true" default:"127.0.0.1:5001"`
}

func main() {
	help := flag.Bool("help", false, "Show help")
	flag.Parse()

	if *help {
		_ = envconfig.Usage("ratchet", &config{})
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		slog.ErrorContext(ctx, "error loading .env file", "error", err)
		os.Exit(1)
	}

	var c config
	if err := envconfig.Process("ratchet", &c); err != nil {
		slog.ErrorContext(ctx, "error processing environment variables", "error", err)
		os.Exit(1)
	}

	// Logging setup
	shortfile := func(groups []string, a slog.Attr) slog.Attr {
		if a.Key == slog.SourceKey {
			s := a.Value.Any().(*slog.Source)
			s.File = path.Base(s.File)
		}
		return a
	}

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		AddSource:   true,
		ReplaceAttr: shortfile,
	}))
	if c.DevMode {
		logger = slog.New(tint.NewHandler(os.Stderr, &tint.Options{
			AddSource:   true,
			Level:       slog.LevelDebug,
			TimeFormat:  time.Kitchen,
			ReplaceAttr: shortfile,
		}))
	}
	slog.SetDefault(logger)
	slog.InfoContext(ctx, "Starting ratchet", "version", versioninfo.Short())

	// Metrics setup
	promExporter, err := prometheus.New()
	if err != nil {
		slog.ErrorContext(ctx, "error setting up Prometheus exporter", "error", err)
		os.Exit(1)
	}
	meterProvider := metric.NewMeterProvider(metric.WithReader(promExporter))
	otel.SetMeterProvider(meterProvider)

	// Database setup
	if c.DevMode {
		if err := storage.StartPostgresContainer(ctx, c.Database); err != nil {
			slog.ErrorContext(ctx, "error setting up dev database", "error", err)
			os.Exit(1)
		}
	}
	db, err := storage.New(ctx, c.Database.URL())
	if err != nil {
		slog.ErrorContext(ctx, "error setting up database", "error", err)
		os.Exit(1)
	}

	// LLM setup
	var openaiClient *openai.Client
	if c.OpenAI.APIKey != "" {
		openaiClient = openai.NewClient(option.WithBaseURL(c.OpenAI.URL), option.WithAPIKey(c.OpenAI.APIKey))
		model, err := openaiClient.Models.Get(ctx, c.OpenAI.Model)
		if err != nil {
			slog.ErrorContext(ctx, "error getting model", "error", err)
			os.Exit(1)
		}

		slog.InfoContext(ctx, "OpenAI model info", "model", model.ID, "owner", model.OwnedBy, "created", model.Created)
	}

	// Bot setup
	bot := internal.New(db)

	// Slack integration setup
	slackIntegration, err := slack_integration.New(ctx, c.SlackAppToken, c.SlackBotToken, bot)
	if err != nil {
		slog.ErrorContext(ctx, "error setting up Slack", "error", err)
		os.Exit(1)
	}

	// Classifier setup
	classifier, err := classifier_worker.New(c.Classifier, bot)
	if err != nil {
		slog.ErrorContext(ctx, "error setting up classifier", "error", err)
		os.Exit(1)
	}

	// Channel onboarding worker setup
	channelOnboardWorker := channel_onboard_worker.New(slackIntegration.SlackClient(), bot)

	// Background job setup
	workers := river.NewWorkers()
	river.AddWorker(workers, classifier)
	river.AddWorker(workers, channelOnboardWorker)
	riverClient, err := background.New(db, workers)
	if err != nil {
		slog.ErrorContext(ctx, "error setting up background worker", "error", err)
		os.Exit(1)
	}

	if err := bot.Init(riverClient); err != nil {
		slog.ErrorContext(ctx, "error initializing bot", "error", err)
		os.Exit(1)
	}

	// HTTP server setup
	handler, err := web.New(ctx, db, riverClient)
	if err != nil {
		slog.ErrorContext(ctx, "error setting up HTTP server", "error", err)
		os.Exit(1)
	}

	server := &http.Server{
		BaseContext: func(listener net.Listener) context.Context { return ctx },
		Addr:        c.HTTPAddr,
		Handler:     handler,
	}

	wg, ctx := errgroup.WithContext(ctx)
	wg.Go(func() error {
		slog.InfoContext(ctx, "Starting river client")
		return riverClient.Start(ctx)
	})
	wg.Go(func() error {
		slog.InfoContext(ctx, "Starting HTTP server", "addr", c.HTTPAddr)
		if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("HTTP server error: %w", err)
		}

		return nil
	})
	wg.Go(func() error {
		slog.InfoContext(ctx, "Starting Slack integration", "bot_user_id", slackIntegration.BotUserID)
		return slackIntegration.Run(ctx)
	})
	wg.Go(func() error {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

		select {
		case <-ctx.Done():
		case <-c:
			slog.InfoContext(ctx, "Shutting down")
			cancel()

			if err := server.Shutdown(ctx); err != nil {
				return fmt.Errorf("error shutting down http server: %w", err)
			}
		}

		return nil
	})

	if err := wg.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		slog.ErrorContext(ctx, "error running server", "error", err)
		os.Exit(1)
	}
}
