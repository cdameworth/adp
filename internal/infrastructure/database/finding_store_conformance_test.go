package database

// Finding-store conformance suite (#10). The identical battery runs against
// the in-memory store (reference semantics), SQLite (always), and PostgreSQL
// (when ADP_TEST_POSTGRES_DSN is set, e.g. in CI).

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/adp/adp/internal/domain/enforcement"
)

func makeFinding(id, ref string) enforcement.Finding {
	now := time.Now().UTC().Truncate(time.Millisecond)
	return enforcement.Finding{
		ID:         id,
		Type:       enforcement.FindingUngovernedCommit,
		Reference:  ref,
		Repo:       "org/repo",
		Ref:        "refs/heads/main",
		Author:     "agent@example.com",
		Reason:     "commit has no ADP governance trail",
		Status:     enforcement.StatusOpen,
		DetectedAt: now,
		UpdatedAt:  now,
	}
}

func runFindingStoreConformance(t *testing.T, st enforcement.FindingStore) {
	t.Helper()

	t.Run("upsert creates and dedups by type+reference", func(t *testing.T) {
		f1 := st.Upsert(makeFinding("f-dup-1", "sha-aaa"))
		if f1.ID != "f-dup-1" {
			t.Fatalf("expected id f-dup-1, got %s", f1.ID)
		}
		// Same (type, reference), new ID: must keep the original record.
		f2 := st.Upsert(makeFinding("f-dup-2", "sha-aaa"))
		if f2.ID != "f-dup-1" {
			t.Errorf("dedup kept wrong id: got %s, want f-dup-1", f2.ID)
		}
		if f2.Status != enforcement.StatusOpen {
			t.Errorf("dedup reset status: %s", f2.Status)
		}
	})

	t.Run("upsert refresh preserves detected_at and updates author", func(t *testing.T) {
		first := st.Upsert(makeFinding("f-ref-1", "sha-bbb"))
		updated := makeFinding("f-ref-2", "sha-bbb")
		updated.Author = "other@example.com"
		got := st.Upsert(updated)
		if got.Author != "other@example.com" {
			t.Errorf("author not refreshed: %s", got.Author)
		}
		if !got.DetectedAt.Equal(first.DetectedAt) {
			t.Errorf("detected_at changed on refresh: got %v, want %v", got.DetectedAt, first.DetectedAt)
		}
	})

	t.Run("distinct references coexist", func(t *testing.T) {
		st.Upsert(makeFinding("f-dist-1", "sha-c1"))
		st.Upsert(makeFinding("f-dist-2", "sha-c2"))
		all := st.List("")
		seen := map[string]bool{}
		for _, f := range all {
			seen[f.ID] = true
		}
		if !seen["f-dist-1"] || !seen["f-dist-2"] {
			t.Errorf("missing findings; got ids %v", seen)
		}
	})

	t.Run("list filters by status and returns newest first", func(t *testing.T) {
		base := time.Now().UTC().Truncate(time.Millisecond)
		old := makeFinding("f-ord-1", "sha-ord-1")
		old.DetectedAt = base.Add(-time.Hour)
		st.Upsert(old)
		newer := makeFinding("f-ord-2", "sha-ord-2")
		newer.DetectedAt = base
		st.Upsert(newer)
		st.SetStatus("f-ord-1", enforcement.StatusResolved)

		open := st.List(enforcement.StatusOpen)
		for _, f := range open {
			if f.Status != enforcement.StatusOpen {
				t.Errorf("status filter leaked %s finding %s", f.Status, f.ID)
			}
		}
		resolved := st.List(enforcement.StatusResolved)
		found := false
		for _, f := range resolved {
			if f.ID == "f-ord-1" {
				found = true
			}
		}
		if !found {
			t.Error("resolved finding missing from filtered list")
		}
		all := st.List("")
		if len(all) >= 2 && all[0].DetectedAt.Before(all[len(all)-1].DetectedAt) {
			t.Error("list is not newest-first")
		}
	})

	t.Run("get and set status", func(t *testing.T) {
		st.Upsert(makeFinding("f-gs-1", "sha-gs-1"))
		if _, ok := st.Get("f-gs-1"); !ok {
			t.Fatal("get returned not-found for existing finding")
		}
		updated, ok := st.SetStatus("f-gs-1", enforcement.StatusAcknowledged)
		if !ok || updated.Status != enforcement.StatusAcknowledged {
			t.Errorf("set status failed: ok=%v status=%s", ok, updated.Status)
		}
		if _, ok := st.Get("no-such-finding"); ok {
			t.Error("get returned found for unknown id")
		}
		if _, ok := st.SetStatus("no-such-finding", enforcement.StatusResolved); ok {
			t.Error("set status succeeded for unknown id")
		}
	})
}

func TestFindingStoreConformance_InMemory(t *testing.T) {
	runFindingStoreConformance(t, enforcement.NewInMemoryFindingStore(0))
}

func TestFindingStoreConformance_SQLite(t *testing.T) {
	client, err := NewSQLiteClient(SQLiteConfig{Path: filepath.Join(t.TempDir(), "findings.db")})
	if err != nil {
		t.Fatalf("sqlite client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	runFindingStoreConformance(t, NewSQLiteFindingStore(client))
}

func TestFindingStoreConformance_Postgres(t *testing.T) {
	dsn := os.Getenv("ADP_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ADP_TEST_POSTGRES_DSN not set; skipping PostgreSQL conformance (runs in CI)")
	}
	client := mustPGClient(t, dsn)
	t.Cleanup(func() { _ = client.Close() })
	runFindingStoreConformance(t, NewPgFindingStore(client))
}

// mustPGClient connects and migrates. Shared helper for PG-gated suites.
func mustPGClient(t *testing.T, dsn string) *PostgresClient {
	t.Helper()
	cfg := pgConfigFromDSN(t, dsn)
	client, err := NewPostgresClient(cfg)
	if err != nil {
		t.Fatalf("postgres client: %v", err)
	}
	if err := client.RunMigrations(t.Context(), "../../../migrations/postgres"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	return client
}
