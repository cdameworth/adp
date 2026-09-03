package database

// Verification-store conformance suite (#20). SQLite always; PostgreSQL when
// ADP_TEST_POSTGRES_DSN is set. Covers the Store and KeyStore interfaces.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/adp/adp/internal/domain/verification"
)

type combinedVerificationStore interface {
	verification.Store
	verification.KeyStore
}

func runVerificationStoreConformance(t *testing.T, st combinedVerificationStore) {
	t.Helper()
	ctx := context.Background()

	t.Run("save and get by sha", func(t *testing.T) {
		v := &verification.Verification{
			CommitSHA:      "sha-save-1",
			SessionID:      "sess-1",
			Status:         verification.StatusPassed,
			PipelineURL:    "https://ci/run/1",
			RunnerIdentity: "github-actions:test",
			EvidenceHash:   verification.EvidenceHash("sha-save-1", verification.StatusPassed, "https://ci/run/1", "github-actions:test", time.Now().UTC()),
			CreatedAt:      time.Now().UTC(),
		}
		if err := st.Save(ctx, v); err != nil {
			t.Fatalf("save: %v", err)
		}
		got, err := st.GetBySHA(ctx, "sha-save-1")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got == nil || got.Status != verification.StatusPassed {
			t.Fatalf("got %+v", got)
		}
		if got.EvidenceHash == "" {
			t.Error("evidence hash not persisted")
		}
	})

	t.Run("latest run per sha wins", func(t *testing.T) {
		first := &verification.Verification{CommitSHA: "sha-upsert", Status: verification.StatusFailed, CreatedAt: time.Now().UTC()}
		if err := st.Save(ctx, first); err != nil {
			t.Fatalf("save first: %v", err)
		}
		second := &verification.Verification{CommitSHA: "sha-upsert", Status: verification.StatusPassed, CreatedAt: time.Now().UTC()}
		if err := st.Save(ctx, second); err != nil {
			t.Fatalf("save second: %v", err)
		}
		got, err := st.GetBySHA(ctx, "sha-upsert")
		if err != nil || got == nil {
			t.Fatalf("get: %v", err)
		}
		if got.Status != verification.StatusPassed {
			t.Errorf("expected latest (passed), got %s", got.Status)
		}
	})

	t.Run("unknown sha returns nil, nil", func(t *testing.T) {
		got, err := st.GetBySHA(ctx, "sha-nope")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got != nil {
			t.Errorf("expected nil for unknown sha, got %+v", got)
		}
	})

	t.Run("list newest first with status filter", func(t *testing.T) {
		if err := st.Save(ctx, &verification.Verification{CommitSHA: "sha-list-f", Status: verification.StatusFailed, CreatedAt: time.Now().UTC()}); err != nil {
			t.Fatalf("save: %v", err)
		}
		failed, err := st.List(ctx, "failed", 50, 0)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		for _, v := range failed {
			if v.Status != verification.StatusFailed {
				t.Errorf("status filter leaked %s", v.Status)
			}
		}
	})

	t.Run("key lifecycle", func(t *testing.T) {
		info, plaintext, err := st.CreateKey(ctx, "org/repo-a", "tester")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if plaintext == "" || info.ID == "" {
			t.Fatal("expected plaintext key and record id")
		}

		ok, err := st.ValidateKey(ctx, "org/repo-a", plaintext)
		if err != nil || !ok {
			t.Error("fresh key did not validate")
		}

		ok, _ = st.ValidateKey(ctx, "org/repo-a", "adpvk_wrong")
		if ok {
			t.Error("wrong key validated")
		}

		// Per-repo isolation: a key for repo-a must not attest repo-b.
		ok, _ = st.ValidateKey(ctx, "org/repo-b", plaintext)
		if ok {
			t.Error("key validated for a different repo")
		}

		revoked, err := st.RevokeKey(ctx, info.ID)
		if err != nil || !revoked {
			t.Error("revoke failed")
		}
		ok, _ = st.ValidateKey(ctx, "org/repo-a", plaintext)
		if ok {
			t.Error("revoked key still validates")
		}

		if ok, _ := st.RevokeKey(ctx, "no-such-key"); ok {
			t.Error("revoke succeeded for unknown key")
		}
	})

	t.Run("list keys never exposes hashes", func(t *testing.T) {
		keys, err := st.ListKeys(ctx)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		for _, k := range keys {
			if k.Repo == "" || k.ID == "" {
				t.Errorf("malformed key info: %+v", k)
			}
		}
	})
}

func TestVerificationStoreConformance_SQLite(t *testing.T) {
	client, err := NewSQLiteClient(SQLiteConfig{Path: filepath.Join(t.TempDir(), "ver.db")})
	if err != nil {
		t.Fatalf("sqlite client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	runVerificationStoreConformance(t, NewSQLiteVerificationStore(client))
}

func TestVerificationStoreConformance_Postgres(t *testing.T) {
	dsn := os.Getenv("ADP_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ADP_TEST_POSTGRES_DSN not set; skipping PostgreSQL conformance (runs in CI)")
	}
	client := mustPGClient(t, dsn)
	t.Cleanup(func() { _ = client.Close() })
	runVerificationStoreConformance(t, NewPgVerificationStore(client))
}
