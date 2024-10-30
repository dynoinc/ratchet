package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/carlmjohnson/versioninfo"
	"github.com/kelseyhightower/envconfig"
	"golang.org/x/sync/errgroup"

	"github.com/rajatgoel/ratchet/internal"
	"github.com/rajatgoel/ratchet/internal/slack"
	"github.com/rajatgoel/ratchet/internal/storage"
	"github.com/rajatgoel/ratchet/internal/web"
)

type Config struct {
	// Database configuration
	storage.DatabaseConfig

	// Slack configuration
	SlackBotToken string `split_words:"true" required:"true"`
	SlackAppToken string `split_words:"true" required:"true"`

	// HTTP configuration
	HTTPAddr string `split_words:"true" default:":5001"`
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log.Println("Running version:", versioninfo.Short())

	var c Config
	if err := envconfig.Process("ratchet", &c); err != nil {
		log.Fatalf("error loading configuration: %v", err)
	}

	// Database setup
	db, err := storage.New(ctx, c.DatabaseConfig)
	if err != nil {
		log.Fatalf("error setting up database: %v", err)
	}

	// Bot setup (the business logic goes here)
	bot, err := internal.New(db)
	if err != nil {
		log.Fatalf("error setting up bot: %v", err)
	}

	// Slack integration setup
	slack, err := slack.New(ctx, c.SlackAppToken, c.SlackBotToken, bot)
	if err != nil {
		log.Fatalf("error setting up Slack: %v", err)
	}

	// HTTP server setup
	handler, err := web.New(db)
	if err != nil {
		log.Fatalf("error setting up HTTP server: %v", err)
	}

	server := &http.Server{
		BaseContext: func(listener net.Listener) context.Context { return ctx },
		Addr:        c.HTTPAddr,
		Handler:     handler,
	}

	wg, ctx := errgroup.WithContext(ctx)
	wg.Go(func() error {
		log.Printf("Starting HTTP server on %s", c.HTTPAddr)
		if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("HTTP server error: %w", err)
		}

		return nil
	})

	wg.Go(func() error {
		log.Printf("Starting bot with ID %s", slack.BotUserID)
		return slack.Run(ctx)
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

	if err := wg.Wait(); err != nil {
		log.Printf("error running server: %v\n", err)
	}
}
