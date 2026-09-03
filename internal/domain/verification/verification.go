// Package verification implements behavioral verification (#20): attested,
// independently-produced evidence that a commit builds and passes tests,
// recorded on the audit trail and enforced at the merge gate.
//
// The load-bearing invariant: evidence comes from a DIFFERENT trust domain
// than the agent (CI runner, not the agent session). An attestation whose
// session_id matches the session that prepared the commit is rejected and
// recorded as a finding — the same trust domain cannot attest its own work.
package verification

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// Status is the outcome of an attested verification run.
type Status string

const (
	StatusPassed Status = "passed"
	StatusFailed Status = "failed"
)

// Verification is an attestation that a commit was built and tested by an
// independent runner. One record per commit SHA (latest run wins).
type Verification struct {
	ID             string    `json:"id"`
	CommitSHA      string    `json:"commit_sha"`
	SessionID      string    `json:"session_id,omitempty"`
	Status         Status    `json:"status"`
	PipelineURL    string    `json:"pipeline_url,omitempty"`
	RunnerIdentity string    `json:"runner_identity,omitempty"`
	EvidenceHash   string    `json:"evidence_hash"`
	CreatedAt      time.Time `json:"created_at"` // when the run completed (attested)
	ReceivedAt     time.Time `json:"received_at"`
}

// KeyInfo describes a per-repo verification key (the hash is never returned).
type KeyInfo struct {
	ID        string     `json:"id"`
	Repo      string     `json:"repo"`
	CreatedBy string     `json:"created_by,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

// Store persists verifications.
type Store interface {
	// Save upserts by commit SHA (latest run for a SHA wins).
	Save(ctx context.Context, v *Verification) error
	// GetBySHA returns (nil, nil) when no attestation exists.
	GetBySHA(ctx context.Context, sha string) (*Verification, error)
	List(ctx context.Context, status string, limit, offset int) ([]*Verification, error)
}

// KeyStore manages per-repo verification keys. Only SHA-256 hashes are stored.
type KeyStore interface {
	// CreateKey stores the hash and returns the record plus the plaintext key
	// (shown to the caller exactly once).
	CreateKey(ctx context.Context, repo, createdBy string) (*KeyInfo, string, error)
	// ValidateKey reports whether key is valid for repo (not revoked).
	ValidateKey(ctx context.Context, repo, key string) (bool, error)
	RevokeKey(ctx context.Context, id string) (bool, error)
	ListKeys(ctx context.Context) ([]*KeyInfo, error)
}

// NewID generates a verification record ID.
func NewID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "ver_" + hex.EncodeToString(b)
}

// GenerateKey creates a new verification key, returning the plaintext (shown
// once) and the SHA-256 hash to store.
func GenerateKey() (plaintext, hash string) {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	plaintext = "adpvk_" + hex.EncodeToString(b)
	sum := sha256.Sum256([]byte(plaintext))
	return plaintext, hex.EncodeToString(sum[:])
}

// HashKey hashes a presented key for comparison against stored hashes.
func HashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// EvidenceHash is a tamper-evident pointer to the run: SHA-256 over the
// canonical payload. ADP stores the hash, not the logs.
func EvidenceHash(commitSHA string, status Status, pipelineURL, runnerIdentity string, completedAt time.Time) string {
	canonical := fmt.Sprintf("%s|%s|%s|%s|%s", commitSHA, string(status), pipelineURL, runnerIdentity, completedAt.UTC().Format(time.RFC3339Nano))
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}
