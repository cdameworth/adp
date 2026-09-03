package verification

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeGov struct {
	verified bool
	err      error
}

func (f fakeGov) IsCommitVerified(context.Context, string) (bool, error) {
	return f.verified, f.err
}

type fakeStore struct {
	bySHA map[string]*Verification
}

func (f *fakeStore) Save(_ context.Context, v *Verification) error {
	f.bySHA[v.CommitSHA] = v
	return nil
}
func (f *fakeStore) GetBySHA(_ context.Context, sha string) (*Verification, error) {
	return f.bySHA[sha], nil
}
func (f *fakeStore) List(context.Context, string, int, int) ([]*Verification, error) {
	return nil, nil
}

func attested(status Status) *Verification {
	return &Verification{
		ID:           NewID(),
		CommitSHA:    "sha-x",
		Status:       status,
		EvidenceHash: EvidenceHash("sha-x", status, "", "", time.Now().UTC()),
		CreatedAt:    time.Now().UTC(),
	}
}

func TestGateVerifier_Matrix(t *testing.T) {
	required := func(context.Context) bool { return true }
	notRequired := func(context.Context) bool { return false }

	tests := []struct {
		name     string
		gov      fakeGov
		store    *fakeStore
		required func(context.Context) bool
		want     bool
		wantErr  bool
	}{
		{"governance fail short-circuits", fakeGov{verified: false}, &fakeStore{bySHA: map[string]*Verification{"sha-x": attested(StatusPassed)}}, required, false, false},
		{"governance error propagates", fakeGov{verified: true, err: errors.New("db down")}, &fakeStore{}, notRequired, false, true},
		{"not required passes through governance", fakeGov{verified: true}, &fakeStore{}, notRequired, true, false},
		{"nil required means never required", fakeGov{verified: true}, &fakeStore{}, nil, true, false},
		{"required + no attestation blocks", fakeGov{verified: true}, &fakeStore{bySHA: map[string]*Verification{}}, required, false, false},
		{"required + failed attestation blocks", fakeGov{verified: true}, &fakeStore{bySHA: map[string]*Verification{"sha-x": attested(StatusFailed)}}, required, false, false},
		{"required + passed attestation verifies", fakeGov{verified: true}, &fakeStore{bySHA: map[string]*Verification{"sha-x": attested(StatusPassed)}}, required, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewGateVerifier(tt.gov, tt.store, tt.required)
			got, err := g.IsCommitVerified(context.Background(), "sha-x")
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
