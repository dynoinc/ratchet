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

	"github.com/carlmjohnson/versioninfo"
	"github.com/kelseyhightower/envconfig"
	"github.com/riverqueue/river"
	"golang.org/x/sync/errgroup"

	"github.com/joho/godotenv"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/background/channel_info_worker"
	"github.com/dynoinc/ratchet/internal/background/classifier_worker"
	"github.com/dynoinc/ratchet/internal/background/ingestion_worker"
	"github.com/dynoinc/ratchet/internal/background/report_worker"
	"github.com/dynoinc/ratchet/internal/llm"
	"github.com/dynoinc/ratchet/internal/slack_integration"
	"github.com/dynoinc/ratchet/internal/storage"
	"github.com/dynoinc/ratchet/internal/web"
)

type Config struct {
	DevMode bool `split_words:"true" default:"true"`

	// Database configuration
	Database storage.DatabaseConfig

	// Classifier configuration
	Classifier classifier_worker.Config

	// Slack configuration
	SlackBotToken string `split_words:"true" required:"true"`
	SlackAppToken string `split_words:"true" required:"true"`

	// HTTP configuration
	HTTPAddr string `split_words:"true" default:"127.0.0.1:5001"`
}

func main() {
	help := flag.Bool("help", false, "Show help")
	flag.Parse()

	if *help {
		envconfig.Usage("ratchet", &Config{})
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wg, ctx := errgroup.WithContext(ctx)

	if _, err := os.Stat(".env"); err == nil {
		if err := godotenv.Load(); err != nil {
			slog.ErrorContext(ctx, "error loading .env file", "error", err)
			os.Exit(1)
		}
	}

	var c Config
	if err := envconfig.Process("ratchet", &c); err != nil {
		slog.ErrorContext(ctx, "error processing environment variables", "error", err)
		os.Exit(1)
	}

	// Logging setup
	handlerOpts := &slog.HandlerOptions{
		AddSource: true,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.SourceKey {
				s := a.Value.Any().(*slog.Source)
				s.File = path.Base(s.File)
			}
			return a
		},
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, handlerOpts))
	if c.DevMode {
		logger = slog.New(slog.NewTextHandler(os.Stderr, handlerOpts))
	}
	slog.SetDefault(logger)
	slog.InfoContext(ctx, "Starting ratchet", "version", versioninfo.Short())

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
	if c.DevMode {
		if err := llm.StartOllamaContainer(ctx); err != nil {
			slog.ErrorContext(ctx, "error setting up ollama", "error", err)
			os.Exit(1)
		}
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
	var classifier river.Worker[background.ClassifierArgs]
	if c.DevMode {
		classifier = classifier_worker.NewDev(ctx, bot)
	} else {
		classifier, err = classifier_worker.New(ctx, c.Classifier, bot)
		if err != nil {
			slog.ErrorContext(ctx, "error setting up classifier", "error", err)
			os.Exit(1)
		}
	}

	// Ingestion worker setup
	ingestionWorker, err := ingestion_worker.New(bot, slackIntegration.SlackClient())
	if err != nil {
		slog.ErrorContext(ctx, "error setting up ingestion worker", "error", err)
		os.Exit(1)
	}

	// Report worker setup
	reportWorker, err := report_worker.New(slackIntegration.SlackClient(), db)
	if err != nil {
		slog.ErrorContext(ctx, "error setting up report worker", "error", err)
		os.Exit(1)
	}

	// Channel info worker setup
	channelInfoWorker := channel_info_worker.New(slackIntegration.SlackClient(), db)

	// Background job setup
	workers := river.NewWorkers()
	river.AddWorker(workers, classifier)
	river.AddWorker(workers, ingestionWorker)
	river.AddWorker(workers, reportWorker)
	river.AddWorker(workers, channelInfoWorker)
	riverClient, err := background.New(db, workers)
	if err != nil {
		slog.ErrorContext(ctx, "error setting up background worker", "error", err)
		os.Exit(1)
	}

	if err := bot.Init(ctx, riverClient); err != nil {
		slog.ErrorContext(ctx, "error initializing bot", "error", err)
		os.Exit(1)
	}

	// Setup periodic jobs (for now only in dev mode)
	if c.DevMode {
		if err := background.Setup(ctx, db, riverClient); err != nil {
			slog.ErrorContext(ctx, "error setting up periodic jobs", "error", err)
			os.Exit(1)
		}
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
				return err
			}
		}

		return nil
	})

	if err := wg.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		slog.ErrorContext(ctx, "error running server", "error", err)
		os.Exit(1)
	}
}
