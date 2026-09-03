package main

import (
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/adp/adp/internal/infrastructure/database"
)

func TestConnectPostgresWithRetry_ExhaustsBudget(t *testing.T) {
	// Point at a port that refuses connections instantly.
	cfg := database.PostgresConfig{
		Host: "127.0.0.1", Port: 1, Database: "x", Username: "x", Password: "x", SSLMode: "disable",
	}
	origBudget, origInterval := pgConnectBudget, pgConnectInterval
	pgConnectBudget, pgConnectInterval = 300*time.Millisecond, 50*time.Millisecond
	defer func() { pgConnectBudget, pgConnectInterval = origBudget, origInterval }()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	start := time.Now()
	client, err := connectPostgresWithRetry(cfg, logger)
	if err == nil || client != nil {
		t.Fatal("expected error and nil client for unreachable database")
	}
	elapsed := time.Since(start)
	if elapsed < 250*time.Millisecond || elapsed > 3*time.Second {
		t.Errorf("retry budget not respected: took %v", elapsed)
	}
}
