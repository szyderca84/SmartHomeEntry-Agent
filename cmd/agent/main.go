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
