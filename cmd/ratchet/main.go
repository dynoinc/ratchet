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

	"github.com/earthboundkid/versioninfo/v2"
	"github.com/getsentry/sentry-go"
	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"
	"github.com/lmittmann/tint"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/sdk/metric"
	"golang.org/x/sync/errgroup"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/background/backfill_thread_worker"
	"github.com/dynoinc/ratchet/internal/background/channel_onboard_worker"
	"github.com/dynoinc/ratchet/internal/background/classifier_worker"
	"github.com/dynoinc/ratchet/internal/background/modules_worker"
	"github.com/dynoinc/ratchet/internal/background/persist_llm_usage_worker"
	"github.com/dynoinc/ratchet/internal/llm"
	"github.com/dynoinc/ratchet/internal/modules"
	"github.com/dynoinc/ratchet/internal/modules/channel_monitor"
	"github.com/dynoinc/ratchet/internal/modules/commands"
	"github.com/dynoinc/ratchet/internal/modules/runbook"
	"github.com/dynoinc/ratchet/internal/slack_integration"
	"github.com/dynoinc/ratchet/internal/storage"
	"github.com/dynoinc/ratchet/internal/web"
)

type config struct {
	DevMode bool `split_words:"true" default:"true"`

	// Database configuration
	Database storage.DatabaseConfig

	// Classifier configuration
	Classifier classifier_worker.Config

	// OpenAI configuration
	OpenAI llm.Config `envconfig:"OPENAI"`

	// Sentry configuration
	SentryDSN string `envconfig:"SENTRY_DSN"`

	// Slack configuration
	Slack slack_integration.Config

	// HTTP configuration
	HTTPAddr string `split_words:"true" default:"127.0.0.1:5001"`

	// Channel Monitor Configuration
	ChannelMonitor channel_monitor.Config `split_words:"true"`
}

func main() {
	help := flag.Bool("help", false, "Show help")
	version := flag.Bool("version", false, "Show version")
	flag.Parse()

	if *help {
		_ = envconfig.Usage("ratchet", &config{})
		return
	}

	if *version {
		fmt.Println(versioninfo.Short())
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
		slog.ErrorContext(ctx, "setting up Prometheus exporter", "error", err)
		os.Exit(1)
	}
	meterProvider := metric.NewMeterProvider(metric.WithReader(promExporter))
	otel.SetMeterProvider(meterProvider)

	// Sentry setup
	if c.SentryDSN != "" {
		if err := sentry.Init(sentry.ClientOptions{
			Dsn: c.SentryDSN,
		}); err != nil {
			slog.ErrorContext(ctx, "setting up Sentry", "error", err)
			os.Exit(1)
		}

		defer sentry.Flush(2 * time.Second)
	}

	// Database setup
	if c.DevMode {
		if err := storage.StartPostgresContainer(ctx, c.Database); err != nil {
			slog.ErrorContext(ctx, "setting up dev database", "error", err)
			os.Exit(1)
		}
	}
	db, err := storage.New(ctx, c.Database.URL())
	if err != nil {
		slog.ErrorContext(ctx, "setting up database", "error", err)
		os.Exit(1)
	}

	// LLM setup
	llmClient, err := llm.New(ctx, c.OpenAI)
	if err != nil {
		slog.ErrorContext(ctx, "setting up LLM client", "error", err)
		os.Exit(1)
	}

	// Bot setup
	bot, err := internal.New(ctx, db)
	if err != nil {
		slog.ErrorContext(ctx, "setting up bot", "error", err)
		os.Exit(1)
	}

	// Slack integration setup
	slackIntegration, err := slack_integration.New(ctx, c.Slack, bot)
	if err != nil {
		slog.ErrorContext(ctx, "setting up Slack integration", "error", err)
		os.Exit(1)
	}

	// Classifier setup
	classifier, err := classifier_worker.New(c.Classifier, bot, llmClient)
	if err != nil {
		slog.ErrorContext(ctx, "setting up classifier", "error", err)
		os.Exit(1)
	}

	// Channel onboarding worker setup
	channelOnboardWorker := channel_onboard_worker.New(bot, slackIntegration)

	// Backfill thread worker setup
	backfillThreadWorker := backfill_thread_worker.New(bot, slackIntegration)

	// Modules worker setup
	modulesWorker := modules_worker.New(bot, []modules.Handler{
		commands.New(bot, slackIntegration, llmClient),
		runbook.New(bot, slackIntegration, llmClient),
		channel_monitor.New(c.ChannelMonitor, bot, slackIntegration, llmClient),
	})

	// LLM usage persistence worker setup
	llmUsagePersistenceWorker := persist_llm_usage_worker.New(bot)

	// Background job setup
	workers := river.NewWorkers()
	river.AddWorker(workers, classifier)
	river.AddWorker(workers, channelOnboardWorker)
	river.AddWorker(workers, backfillThreadWorker)
	river.AddWorker(workers, modulesWorker)
	river.AddWorker(workers, llmUsagePersistenceWorker)

	// Start River client
	riverClient, err := river.NewClient(riverpgxv5.New(db), &river.Config{
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 100},
		},
		Workers: workers,
	})
	if err != nil {
		slog.ErrorContext(ctx, "failed to create river client", "error", err)
		os.Exit(1)
	}

	if err := bot.Init(riverClient); err != nil {
		slog.ErrorContext(ctx, "initializing bot", "error", err)
		os.Exit(1)
	}

	// Set the river client on the LLM client for usage tracking
	llmClient.SetRiverClient(llm.NewRiverClientAdapter(riverClient))

	// HTTP server setup
	handler, err := web.New(ctx, db, riverClient, slackIntegration, llmClient)
	if err != nil {
		slog.ErrorContext(ctx, "setting up HTTP server", "error", err)
		os.Exit(1)
	}

	server := &http.Server{
		BaseContext:       func(listener net.Listener) context.Context { return ctx },
		Addr:              c.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,  // Prevent Slowloris attacks
		ReadTimeout:       30 * time.Second,  // Maximum duration for reading entire request
		WriteTimeout:      30 * time.Second,  // Maximum duration before timing out writes
		IdleTimeout:       120 * time.Second, // Maximum amount of time to wait for the next request
		MaxHeaderBytes:    1 << 20,           // 1MB - Prevent header size attacks
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
		slog.InfoContext(ctx, "Starting Slack integration", "bot_user_id", slackIntegration.BotUserID())
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
				return fmt.Errorf("shutting down http server: %w", err)
			}
		}

		return nil
	})

	if err := wg.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		slog.ErrorContext(ctx, "running server", "error", err)
		os.Exit(1)
	}
}
