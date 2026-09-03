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
	// The meaningful assertion: it actually retried through the budget rather
	// than failing on the first attempt. The upper bound only guards against
	// a hang, so it stays generous for loaded CI runners.
	elapsed := time.Since(start)
	if elapsed < 250*time.Millisecond {
		t.Errorf("failed too fast — retry loop did not run (took %v)", elapsed)
	}
	if elapsed > 30*time.Second {
		t.Errorf("retry loop hung: took %v", elapsed)
	}
}
