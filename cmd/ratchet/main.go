package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"

	"github.com/kelseyhightower/envconfig"

	"github.com/rajatgoel/ratchet/internal"
)

type Config struct {
	// Database configuration
	DatabaseHost string `split_words:"true"`
	DatabasePort int    `split_words:"true"`
	DatabaseUser string `split_words:"true"`
	DatabasePass string `split_words:"true"`
	DatabaseName string `split_words:"true"`
	// Slack configuration
	SlackBotToken string `split_words:"true" required:"true"`
	// HTTP configuration
	HTTPAddr string `split_words:"true" default:":5001"`
}

func main() {
	var c Config
	if err := envconfig.Process("ratchet", &c); err != nil {
		log.Fatalf("error loading configuration: %v", err)
	}

	// Slack API test
	if err := internal.TestSlackAPIConnectivity(c.SlackBotToken); err != nil {
		log.Printf("Slack API test failed: %v", err)
	} else {
		log.Println("Slack API test passed")
	}

	// Database test
	dbURL := &url.URL{
		Scheme: "postgres", // Use the appropriate scheme for your database
		User:   url.UserPassword(c.DatabaseUser, c.DatabasePass),
		Host:   fmt.Sprintf("%s:%d", c.DatabaseHost, c.DatabasePort),
		Path:   c.DatabaseName,
	}
	if err := internal.TestDBConnection(dbURL.String()); err != nil {
		log.Printf("Database test failed: %v", err)
	} else {
		log.Println("Database test passed")
	}

	log.Printf("Starting HTTP server on %s", c.HTTPAddr)
	server := &http.Server{
		Addr:    c.HTTPAddr,
		Handler: internal.NewHandler(),
	}
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("HTTP server error: %v", err)
	}
}
