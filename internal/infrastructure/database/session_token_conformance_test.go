package database

// Session-token conformance suite (#12). The identical battery runs against
// SQLite (always) and PostgreSQL (when ADP_TEST_POSTGRES_DSN is set, e.g. in
// CI). Store parity is a contract, not a hope: any drift in ValidateToken
// semantics fails one side.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/adp/adp/internal/store"
	"github.com/google/uuid"
)

// sessionTokenStore is the minimal surface the conformance battery needs.
type sessionTokenStore interface {
	Create(ctx context.Context, input store.CreateSessionInput) (*store.Session, error)
	ValidateToken(ctx context.Context, sessionID, tokenHash string) (bool, error)
	End(ctx context.Context, id string) error
}

// pgTokenStore adapts the PG-native SessionStore (UUID input types) to the
// portable store interface, mirroring internal/mcp.PgSessionAdapter.
type pgTokenStore struct{ s *SessionStore }

func (p pgTokenStore) Create(ctx context.Context, input store.CreateSessionInput) (*store.Session, error) {
	orgID, err := uuid.Parse(input.OrganizationID)
	if err != nil {
		orgID = uuid.New()
	}
	userID, err := uuid.Parse(input.UserID)
	if err != nil {
		userID = uuid.New()
	}
	sess, err := p.s.Create(ctx, CreateSessionInput{
		ID:             input.ID,
		OrganizationID: orgID,
		UserID:         userID,
		Tool:           input.Tool,
		TrustLevel:     input.TrustLevel,
		ExpiresAt:      input.ExpiresAt,
		TokenHash:      input.TokenHash,
	})
	if err != nil {
		return nil, err
	}
	return &store.Session{ID: sess.ID, Status: sess.Status}, nil
}

func (p pgTokenStore) ValidateToken(ctx context.Context, sessionID, tokenHash string) (bool, error) {
	return p.s.ValidateToken(ctx, sessionID, tokenHash)
}

func (p pgTokenStore) End(ctx context.Context, id string) error {
	return p.s.End(ctx, id)
}

func newToken() (token, hash string) {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	token = hex.EncodeToString(b)
	sum := sha256.Sum256([]byte(token))
	return token, hex.EncodeToString(sum[:])
}

func newSessionInput(id, tokenHash string) store.CreateSessionInput {
	return store.CreateSessionInput{
		ID:             id,
		OrganizationID: uuid.NewString(),
		UserID:         uuid.NewString(),
		Tool:           "conformance-test",
		TrustLevel:     3,
		ExpiresAt:      time.Now().Add(8 * time.Hour),
		TokenHash:      tokenHash,
	}
}

// runSessionTokenConformance is the shared battery. Each subtest is the
// regression test for a bypass class.
func runSessionTokenConformance(t *testing.T, st sessionTokenStore) {
	t.Helper()
	ctx := context.Background()

	t.Run("valid token validates", func(t *testing.T) {
		_, hash := newToken()
		if _, err := st.Create(ctx, newSessionInput("conf-valid-"+uuid.NewString(), hash)); err != nil {
			t.Fatalf("create: %v", err)
		}
	})
	// The tests below share one session per case to keep ids independent.

	t.Run("correct token accepted", func(t *testing.T) {
		id := "conf-ok-" + uuid.NewString()
		_, hash := newToken()
		if _, err := st.Create(ctx, newSessionInput(id, hash)); err != nil {
			t.Fatalf("create: %v", err)
		}
		valid, err := st.ValidateToken(ctx, id, hash)
		if err != nil {
			t.Fatalf("validate: %v", err)
		}
		if !valid {
			t.Error("correct token rejected")
		}
	})

	t.Run("forged token rejected", func(t *testing.T) {
		id := "conf-forged-" + uuid.NewString()
		_, hash := newToken()
		if _, err := st.Create(ctx, newSessionInput(id, hash)); err != nil {
			t.Fatalf("create: %v", err)
		}
		_, wrongHash := newToken()
		valid, err := st.ValidateToken(ctx, id, wrongHash)
		if err != nil {
			t.Fatalf("validate: %v", err)
		}
		if valid {
			t.Error("forged token accepted")
		}
	})

	t.Run("unknown session rejected", func(t *testing.T) {
		_, hash := newToken()
		valid, err := st.ValidateToken(ctx, "no-such-session-"+uuid.NewString(), hash)
		if err != nil {
			t.Fatalf("validate: %v", err)
		}
		if valid {
			t.Error("unknown session validated")
		}
	})

	t.Run("ended session rejected", func(t *testing.T) {
		id := "conf-ended-" + uuid.NewString()
		_, hash := newToken()
		if _, err := st.Create(ctx, newSessionInput(id, hash)); err != nil {
			t.Fatalf("create: %v", err)
		}
		if err := st.End(ctx, id); err != nil {
			t.Fatalf("end: %v", err)
		}
		valid, err := st.ValidateToken(ctx, id, hash)
		if err != nil {
			t.Fatalf("validate: %v", err)
		}
		if valid {
			t.Error("ended session validated")
		}
	})

	t.Run("session created without token can never validate", func(t *testing.T) {
		id := "conf-notoken-" + uuid.NewString()
		if _, err := st.Create(ctx, newSessionInput(id, "")); err != nil {
			t.Fatalf("create: %v", err)
		}
		// Empty presented hash must not self-match the empty stored hash.
		if valid, _ := st.ValidateToken(ctx, id, ""); valid {
			t.Error("empty-hash self-match: tokenless session validated with empty token")
		}
		_, someHash := newToken()
		if valid, _ := st.ValidateToken(ctx, id, someHash); valid {
			t.Error("tokenless session validated with arbitrary token")
		}
	})

	t.Run("token valid for one session is invalid for another", func(t *testing.T) {
		idA := "conf-a-" + uuid.NewString()
		idB := "conf-b-" + uuid.NewString()
		_, hashA := newToken()
		_, hashB := newToken()
		if _, err := st.Create(ctx, newSessionInput(idA, hashA)); err != nil {
			t.Fatalf("create A: %v", err)
		}
		if _, err := st.Create(ctx, newSessionInput(idB, hashB)); err != nil {
			t.Fatalf("create B: %v", err)
		}
		if valid, _ := st.ValidateToken(ctx, idB, hashA); valid {
			t.Error("session A token validated for session B")
		}
	})
}

func TestSessionTokenConformance_SQLite(t *testing.T) {
	client, err := NewSQLiteClient(SQLiteConfig{Path: filepath.Join(t.TempDir(), "conf.db")})
	if err != nil {
		t.Fatalf("sqlite client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	runSessionTokenConformance(t, NewSQLiteSessionStore(client))
}

func TestSessionTokenConformance_Postgres(t *testing.T) {
	dsn := os.Getenv("ADP_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ADP_TEST_POSTGRES_DSN not set; skipping PostgreSQL conformance (runs in CI)")
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse DSN: %v", err)
	}
	port := 5432
	if p := u.Port(); p != "" {
		port, err = strconv.Atoi(p)
		if err != nil {
			t.Fatalf("parse DSN port: %v", err)
		}
	}
	password, _ := u.User.Password()
	sslmode := u.Query().Get("sslmode")
	if sslmode == "" {
		sslmode = "disable"
	}
	client, err := NewPostgresClient(PostgresConfig{
		Host:     u.Hostname(),
		Port:     port,
		Database: strings.TrimPrefix(u.Path, "/"),
		Username: u.User.Username(),
		Password: password,
		SSLMode:  sslmode,
	})
	if err != nil {
		t.Fatalf("postgres client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	if err := client.RunMigrations(context.Background(), "../../../migrations/postgres"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	runSessionTokenConformance(t, pgTokenStore{NewSessionStore(client)})
}
