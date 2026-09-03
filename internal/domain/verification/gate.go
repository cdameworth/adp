package verification

import "context"

// GovernanceVerifier reports whether a commit has a verified governance trail
// (satisfied by both commit stores; mirrors api.CommitVerifier without
// importing the api package).
type GovernanceVerifier interface {
	IsCommitVerified(ctx context.Context, sha string) (bool, error)
}

// GateVerifier composes governance verification with behavioral verification
// for the merge gate: a commit is verified iff it has a governance trail AND
// (when required) a passed attestation from an independent runner.
type GateVerifier struct {
	gov      GovernanceVerifier
	store    Store
	required func(ctx context.Context) bool
}

// NewGateVerifier composes the two checks. required is evaluated per call so
// policy toggles take effect without a restart; when nil, behavioral
// verification is never required.
func NewGateVerifier(gov GovernanceVerifier, store Store, required func(ctx context.Context) bool) *GateVerifier {
	return &GateVerifier{gov: gov, store: store, required: required}
}

// IsCommitVerified implements the merge-gate check. Fails closed: governance
// errors and missing/failed attestations all mean "not verified".
func (g *GateVerifier) IsCommitVerified(ctx context.Context, sha string) (bool, error) {
	ok, err := g.gov.IsCommitVerified(ctx, sha)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	if g.required == nil || !g.required(ctx) {
		return true, nil
	}
	v, err := g.store.GetBySHA(ctx, sha)
	if err != nil {
		return false, err
	}
	if v == nil {
		return false, nil // missing_behavioral_verification
	}
	return v.Status == StatusPassed, nil
}
