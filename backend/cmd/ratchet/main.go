package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
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
	// CORS configuration
	CORSAllowedOrigins string `split_words:"true" default:"http://localhost:8080"`
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
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("Running version:", versioninfo.Short())

	if _, err := os.Stat(".env"); err == nil {
		if err := godotenv.Load(); err != nil {
			log.Fatal("Error loading .env file")
		}
	}

	var c Config
	if err := envconfig.Process("ratchet", &c); err != nil {
		log.Fatalf("error loading configuration: %v", err)
	}

	// Database setup
	if c.DevMode {
		if err := storage.StartPostgresContainer(ctx, c.Database); err != nil {
			log.Fatalf("error setting up dev database: %v", err)
		}
	}
	db, err := storage.New(ctx, c.Database.URL())
	if err != nil {
		log.Fatalf("error setting up database: %v", err)
	}

	// LLM setup
	if c.DevMode {
		if err := llm.StartOllamaContainer(ctx); err != nil {
			log.Fatalf("error setting up ollama: %v", err)
		}
	}

	// Bot setup
	bot := internal.New(db)

	// Slack integration setup
	slackIntegration, err := slack_integration.New(ctx, c.SlackAppToken, c.SlackBotToken, bot)
	if err != nil {
		log.Fatalf("error setting up Slack: %v", err)
	}

	// Classifier setup
	var classifier river.Worker[background.ClassifierArgs]
	if c.DevMode {
		classifier = classifier_worker.NewDev(ctx, bot)
	} else {
		classifier, err = classifier_worker.New(ctx, c.Classifier, bot)
		if err != nil {
			log.Fatalf("error setting up classifier: %v", err)
		}
	}

	// Ingestion worker setup
	ingestionWorker, err := ingestion_worker.New(bot, slackIntegration.SlackClient())
	if err != nil {
		log.Fatalf("error setting up ingestion worker: %v", err)
	}

	// Report worker setup
	reportWorker, err := report_worker.New(slackIntegration.SlackClient(), db)
	if err != nil {
		log.Fatalf("error setting up report worker: %v", err)
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
		log.Fatalf("error setting up background worker: %v", err)
	}

	if err := bot.Init(ctx, riverClient); err != nil {
		log.Fatalf("error initializing bot: %v", err)
	}

	// Setup periodic jobs (for now only in dev mode)
	if c.DevMode {
		if err := background.Setup(ctx, db, riverClient); err != nil {
			log.Fatalf("error setting up periodic jobs: %v", err)
		}
	}

	// HTTP server setup
	handler, err := web.New(ctx, db, riverClient, web.Config{
		CORSAllowedOrigins: c.CORSAllowedOrigins,
	})
	if err != nil {
		log.Fatalf("error setting up HTTP server: %v", err)
	}

	server := &http.Server{
		BaseContext: func(listener net.Listener) context.Context { return ctx },
		Addr:        c.HTTPAddr,
		Handler:     handler,
	}

	wg.Go(func() error {
		log.Printf("Starting river client")
		return riverClient.Start(ctx)
	})
	wg.Go(func() error {
		log.Printf("Starting HTTP server on %s", c.HTTPAddr)
		if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("HTTP server error: %w", err)
		}

		return nil
	})

	wg.Go(func() error {
		log.Printf("Starting bot with ID %s", slackIntegration.BotUserID)
		return slackIntegration.Run(ctx)
	})
	wg.Go(func() error {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

		select {
		case <-ctx.Done():
		case <-c:
			log.Println("Shutting down")
			cancel()

			if err := server.Shutdown(ctx); err != nil {
				return err
			}
		}

		return nil
	})

	if err := wg.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("error running server: %v\n", err)
	}
}
