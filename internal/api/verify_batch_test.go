package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeCommitVerifier struct {
	verified map[string]bool
}

func (f *fakeCommitVerifier) IsCommitVerified(_ context.Context, sha string) (bool, error) {
	return f.verified[sha], nil
}

func postVerifyBatch(t *testing.T, cv CommitVerifier, body string) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/commits/verify-batch", strings.NewReader(body))
	verifyBatchHandler(cv)(rr, req)
	return rr
}

func TestVerifyBatch_ServiceUnavailableWhenNil(t *testing.T) {
	if rr := postVerifyBatch(t, nil, `{"shas":["a"]}`); rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", rr.Code)
	}
}

func TestVerifyBatch_AllVerifiedAllowed(t *testing.T) {
	cv := &fakeCommitVerifier{verified: map[string]bool{"a": true, "b": true}}
	rr := postVerifyBatch(t, cv, `{"shas":["a","b"]}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}
	var resp struct {
		Allowed    bool     `json:"allowed"`
		Total      int      `json:"total"`
		Verified   int      `json:"verified"`
		Unverified []string `json:"unverified"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Allowed || resp.Total != 2 || resp.Verified != 2 || len(resp.Unverified) != 0 {
		t.Fatalf("unexpected: %+v", resp)
	}
}

func TestVerifyBatch_BlocksWhenAnyUngoverned(t *testing.T) {
	cv := &fakeCommitVerifier{verified: map[string]bool{"a": true}}
	rr := postVerifyBatch(t, cv, `{"shas":["a","b","c"]}`)
	var resp struct {
		Allowed    bool     `json:"allowed"`
		Unverified []string `json:"unverified"`
	}
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Allowed {
		t.Fatal("must not be allowed when a commit lacks a governance trail")
	}
	if len(resp.Unverified) != 2 {
		t.Fatalf("want 2 unverified, got %v", resp.Unverified)
	}
}

func TestVerifyBatch_BadRequest(t *testing.T) {
	cv := &fakeCommitVerifier{}
	if rr := postVerifyBatch(t, cv, `{"shas":[]}`); rr.Code != http.StatusBadRequest {
		t.Fatalf("empty shas: want 400, got %d", rr.Code)
	}
	if rr := postVerifyBatch(t, cv, `not json`); rr.Code != http.StatusBadRequest {
		t.Fatalf("bad json: want 400, got %d", rr.Code)
	}
}
