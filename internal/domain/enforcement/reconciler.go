// Package enforcement provides the reconciliation backstop: it detects activity
// that bypassed ADP governance (e.g. commits with no governance trail) and
// records them as findings for the Backstage console.
//
// This is detection, not prevention — it complements the merge gate and the
// gateway interceptor by catching gaps (repos without the gate, direct pushes,
// bypasses) after the fact.
package enforcement

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"
)

// FindingType enumerates kinds of ungoverned activity.
type FindingType string

const (
	FindingUngovernedCommit FindingType = "ungoverned_commit"
)

// FindingStatus is the lifecycle of a finding.
type FindingStatus string

const (
	StatusOpen         FindingStatus = "open"
	StatusAcknowledged FindingStatus = "acknowledged"
	StatusResolved     FindingStatus = "resolved"
)

// Finding is a detected piece of ungoverned activity.
type Finding struct {
	ID         string        `json:"id"`
	Type       FindingType   `json:"type"`
	Reference  string        `json:"reference"` // e.g. commit SHA
	Repo       string        `json:"repo,omitempty"`
	Ref        string        `json:"ref,omitempty"` // branch/ref
	Author     string        `json:"author,omitempty"`
	Reason     string        `json:"reason"`
	Status     FindingStatus `json:"status"`
	DetectedAt time.Time     `json:"detected_at"`
	UpdatedAt  time.Time     `json:"updated_at"`
}

// ObservedCommit is a commit seen in a repo (e.g. from a push webhook).
type ObservedCommit struct {
	SHA         string    `json:"sha"`
	Repo        string    `json:"repo,omitempty"`
	Ref         string    `json:"ref,omitempty"`
	Author      string    `json:"author,omitempty"`
	CommittedAt time.Time `json:"committed_at,omitempty"`
}

// CommitVerifier reports whether a commit SHA has a verified governance trail.
type CommitVerifier interface {
	IsCommitVerified(ctx context.Context, sha string) (bool, error)
}

// FindingStore persists findings. Implementations must be safe for concurrent
// use. Upsert deduplicates by (Type, Reference).
type FindingStore interface {
	Upsert(f Finding) Finding
	List(status FindingStatus) []Finding
	Get(id string) (Finding, bool)
	SetStatus(id string, status FindingStatus) (Finding, bool)
}

// Reconciler compares observed activity against ADP's governance records and
// records findings for anything ungoverned.
type Reconciler struct {
	verifier CommitVerifier
	store    FindingStore
	now      func() time.Time
	idFn     func() string
}

// NewReconciler creates a Reconciler.
func NewReconciler(v CommitVerifier, s FindingStore) *Reconciler {
	return &Reconciler{verifier: v, store: s, now: time.Now, idFn: randID}
}

// ObserveCommits checks each observed commit and records a finding for any that
// lack a verified governance trail. Governed commits are ignored. Returns the
// findings recorded (or updated) this call.
func (r *Reconciler) ObserveCommits(ctx context.Context, commits []ObservedCommit) ([]Finding, error) {
	out := []Finding{}
	for _, c := range commits {
		if c.SHA == "" {
			continue
		}
		governed, err := r.verifier.IsCommitVerified(ctx, c.SHA)
		if err != nil {
			return out, err
		}
		if governed {
			continue
		}
		now := r.now()
		out = append(out, r.store.Upsert(Finding{
			ID:         r.idFn(),
			Type:       FindingUngovernedCommit,
			Reference:  c.SHA,
			Repo:       c.Repo,
			Ref:        c.Ref,
			Author:     c.Author,
			Reason:     "commit has no ADP governance trail (never prepared/registered)",
			Status:     StatusOpen,
			DetectedAt: now,
			UpdatedAt:  now,
		}))
	}
	return out, nil
}

// Findings returns recorded findings, optionally filtered by status ("" = all).
func (r *Reconciler) Findings(status FindingStatus) []Finding {
	return r.store.List(status)
}

// Resolve transitions a finding to a new status.
func (r *Reconciler) Resolve(id string, status FindingStatus) (Finding, bool) {
	return r.store.SetStatus(id, status)
}

func randID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "find_" + hex.EncodeToString(b)
}
