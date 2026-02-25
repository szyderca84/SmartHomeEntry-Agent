// SmartHomeEntry Agent — entry point.
//
// Required environment variables:
//
//	SMARTHOMEENTRY_API_URL      full HTTPS URL of the control plane (e.g. https://api.example.com)
//	SMARTHOMEENTRY_INSTALL_TOKEN install token issued by the control plane
//
// The agent logs to /var/log/smarthomeentry.log and stderr (captured by journald).
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/smarthomeentry/agent/internal/agent"
)

const logFilePath = "/var/log/smarthomeentry.log"

func main() {
	if err := setupLogging(); err != nil {
		// Non-fatal: continue with stderr-only logging.
		fmt.Fprintf(os.Stderr, "warning: cannot open log file %s: %v\n", logFilePath, err)
	}

	apiURL := os.Getenv("SMARTHOMEENTRY_API_URL")
	if apiURL == "" {
		log.Fatal("SMARTHOMEENTRY_API_URL environment variable is required")
	}

	token := os.Getenv("SMARTHOMEENTRY_INSTALL_TOKEN")
	if token == "" {
		log.Fatal("SMARTHOMEENTRY_INSTALL_TOKEN environment variable is required")
	}

	// Optional: override local home automation server address (default: localhost:8080)
	localAddr := os.Getenv("SMARTHOMEENTRY_LOCAL_ADDR")

	a, err := agent.New(apiURL, token, localAddr)
	if err != nil {
		log.Fatalf("agent init: %v", err)
	}
	defer a.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := a.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("agent error: %v", err)
	}

	log.Println("SmartHomeEntry Agent stopped cleanly")
}

// setupLogging configures the standard logger to write to both stderr
// (captured by journald) and the on-disk log file.
func setupLogging() error {
	f, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	log.SetOutput(io.MultiWriter(os.Stderr, f))
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[smarthomeentry-agent] ")
	return nil
}
