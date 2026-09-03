package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/adp/adp/internal/domain/enforcement"
	"github.com/adp/adp/internal/domain/verification"
)

// --- fakes ---

type fakeVerificationStore struct {
	bySHA  map[string]*verification.Verification
	byID   map[string]*verification.KeyInfo
	hashes map[string]string // repo|hash -> keyID
}

func newFakeVerificationStore() *fakeVerificationStore {
	return &fakeVerificationStore{bySHA: map[string]*verification.Verification{}, byID: map[string]*verification.KeyInfo{}, hashes: map[string]string{}}
}

func (f *fakeVerificationStore) Save(_ context.Context, v *verification.Verification) error {
	f.bySHA[v.CommitSHA] = v
	return nil
}
func (f *fakeVerificationStore) GetBySHA(_ context.Context, sha string) (*verification.Verification, error) {
	return f.bySHA[sha], nil
}
func (f *fakeVerificationStore) List(context.Context, string, int, int) ([]*verification.Verification, error) {
	return nil, nil
}
func (f *fakeVerificationStore) CreateKey(_ context.Context, repo, createdBy string) (*verification.KeyInfo, string, error) {
	plain, hash := verification.GenerateKey()
	info := &verification.KeyInfo{ID: verification.NewID(), Repo: repo, CreatedBy: createdBy, CreatedAt: time.Now().UTC()}
	f.byID[info.ID] = info
	f.hashes[repo+"|"+hash] = info.ID
	return info, plain, nil
}
func (f *fakeVerificationStore) ValidateKey(_ context.Context, repo, key string) (bool, error) {
	id, ok := f.hashes[repo+"|"+verification.HashKey(key)]
	if !ok {
		return false, nil
	}
	return f.byID[id].RevokedAt == nil, nil
}
func (f *fakeVerificationStore) RevokeKey(_ context.Context, id string) (bool, error) {
	k, ok := f.byID[id]
	if !ok || k.RevokedAt != nil {
		return false, nil
	}
	now := time.Now().UTC()
	k.RevokedAt = &now
	return true, nil
}
func (f *fakeVerificationStore) ListKeys(context.Context) ([]*verification.KeyInfo, error) {
	var out []*verification.KeyInfo
	for _, k := range f.byID {
		out = append(out, k)
	}
	return out, nil
}

func TestIngest_KeyRequired(t *testing.T) {
	st := newFakeVerificationStore()
	h := NewVerificationHandler(st, st, nil, nil, nil, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/verifications", strings.NewReader(`{"repo":"a/b","commit_sha":"sha1","status":"passed"}`))
	h.Ingest(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestIngest_RejectsBadKey(t *testing.T) {
	st := newFakeVerificationStore()
	h := NewVerificationHandler(st, st, nil, nil, nil, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/verifications",
		strings.NewReader(`{"repo":"a/b","commit_sha":"sha1","status":"passed"}`))
	req.Header.Set("X-Verification-Key", "adpvk_wrong")
	h.Ingest(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestIngest_AcceptsValidKeyAndFailedRuns(t *testing.T) {
	st := newFakeVerificationStore()
	_, key, _ := st.CreateKey(context.Background(), "a/b", "admin")
	h := NewVerificationHandler(st, st, nil, nil, nil, nil)
	h.now = func() time.Time { return time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC) }

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/verifications",
		strings.NewReader(`{"repo":"a/b","commit_sha":"sha1","status":"failed","pipeline_url":"https://ci/1"}`))
	req.Header.Set("X-Verification-Key", key)
	h.Ingest(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	v, _ := st.GetBySHA(context.Background(), "sha1")
	if v == nil || v.Status != verification.StatusFailed {
		t.Fatal("failed run not recorded — failure is evidence too")
	}
	if v.EvidenceHash == "" {
		t.Error("evidence hash not computed")
	}
}

func TestIngest_SelfAttestationRejectedAndFlagged(t *testing.T) {
	st := newFakeVerificationStore()
	_, key, _ := st.CreateKey(context.Background(), "a/b", "admin")
	findings := enforcement.NewInMemoryFindingStore(0)
	lookup := func(_ context.Context, sha string) (string, error) { return "sess-prep", nil }
	h := NewVerificationHandler(st, st, lookup, findings, nil, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/verifications",
		strings.NewReader(`{"repo":"a/b","commit_sha":"sha2","session_id":"sess-prep","status":"passed"}`))
	req.Header.Set("X-Verification-Key", key)
	h.Ingest(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for self-attestation, got %d", rec.Code)
	}
	if v, _ := st.GetBySHA(context.Background(), "sha2"); v != nil {
		t.Error("self-attestation was saved")
	}
	all := findings.List("")
	seen := false
	for _, f := range all {
		if f.Type == enforcement.FindingSelfAttestation && f.Reference == "sha2" {
			seen = true
		}
	}
	if !seen {
		t.Error("self-attestation attempt not recorded as finding")
	}
}

func TestIngest_DifferentSessionAllowed(t *testing.T) {
	st := newFakeVerificationStore()
	_, key, _ := st.CreateKey(context.Background(), "a/b", "admin")
	lookup := func(_ context.Context, sha string) (string, error) { return "sess-prep", nil }
	h := NewVerificationHandler(st, st, lookup, nil, nil, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/verifications",
		strings.NewReader(`{"repo":"a/b","commit_sha":"sha3","session_id":"ci-runner","status":"passed"}`))
	req.Header.Set("X-Verification-Key", key)
	h.Ingest(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("independent attestation rejected: %d %s", rec.Code, rec.Body.String())
	}
}

func TestCommitStatus_Composition(t *testing.T) {
	st := newFakeVerificationStore()
	gov := func(_ context.Context, sha string) (bool, error) { return true, nil }
	required := func(context.Context) bool { return true }
	h := NewVerificationHandler(st, st, nil, nil, gov, required)

	// No attestation → not merge ready
	req := httptest.NewRequest(http.MethodGet, "/v1/commits/sha4/verification-status", nil)
	req.SetPathValue("sha", "sha4")
	rec := httptest.NewRecorder()
	h.CommitStatus(rec, req)
	var resp struct {
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if resp.Data["merge_ready"] != false {
		t.Error("merge_ready without attestation")
	}

	// Passed attestation → merge ready
	st.Save(context.Background(), &verification.Verification{
		CommitSHA: "sha4", Status: verification.StatusPassed, CreatedAt: time.Now().UTC(),
	})
	rec = httptest.NewRecorder()
	h.CommitStatus(rec, req)
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if resp.Data["merge_ready"] != true {
		t.Errorf("expected merge_ready with passed attestation: %v", resp.Data)
	}
}

func TestKeyAdmin_RequiresAdmin(t *testing.T) {
	st := newFakeVerificationStore()
	h := NewVerificationHandler(st, st, nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/verification-keys", strings.NewReader(`{"repo":"a/b"}`))
	rec := httptest.NewRecorder()
	h.CreateKey(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without admin context, got %d", rec.Code)
	}
}
