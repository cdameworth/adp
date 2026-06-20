package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/adp/adp/internal/store"
)

type fakeDocStore struct {
	byCategory map[string][]*store.DocRecord
	bySession  map[string][]*store.DocRecord
	byID       map[string]*store.DocRecord
}

func (f *fakeDocStore) Save(context.Context, store.DocRecord) error { return nil }
func (f *fakeDocStore) Get(_ context.Context, id string) (*store.DocRecord, error) {
	return f.byID[id], nil
}
func (f *fakeDocStore) ListByCategory(_ context.Context, c string, _ int) ([]*store.DocRecord, error) {
	return f.byCategory[c], nil
}
func (f *fakeDocStore) ListBySession(_ context.Context, s string) ([]*store.DocRecord, error) {
	return f.bySession[s], nil
}
func (f *fakeDocStore) Search(context.Context, string, int) ([]*store.DocRecord, error) {
	return nil, nil
}

type docListResp struct {
	Items []store.DocRecord `json:"items"`
	Total int               `json:"total"`
	Limit int               `json:"limit"`
}

func TestDocsListHandler_ServiceUnavailableWhenNil(t *testing.T) {
	rr := httptest.NewRecorder()
	docsListHandler(nil)(rr, httptest.NewRequest(http.MethodGet, "/v1/docs", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 when no doc store, got %d", rr.Code)
	}
}

func TestDocsListHandler_CategoryAndSession(t *testing.T) {
	now := time.Now()
	fs := &fakeDocStore{
		byCategory: map[string][]*store.DocRecord{
			"session_summary": {{ID: "d1", Category: "session_summary", Title: "Session abc summary", Content: "# Summary", CreatedAt: now}},
		},
		bySession: map[string][]*store.DocRecord{
			"sess-1": {{ID: "d2", SessionID: "sess-1", Category: "risk_report", Title: "Risk report", Content: "# Risk", CreatedAt: now}},
		},
	}

	rr := httptest.NewRecorder()
	docsListHandler(fs)(rr, httptest.NewRequest(http.MethodGet, "/v1/docs", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}
	var resp docListResp
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 1 || resp.Items[0].ID != "d1" {
		t.Fatalf("default category list wrong: %+v", resp)
	}

	rr = httptest.NewRecorder()
	docsListHandler(fs)(rr, httptest.NewRequest(http.MethodGet, "/v1/docs?session_id=sess-1", nil))
	resp = docListResp{}
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Total != 1 || resp.Items[0].ID != "d2" {
		t.Fatalf("session list wrong: %+v", resp)
	}
}

func TestDocGetHandler(t *testing.T) {
	fs := &fakeDocStore{byID: map[string]*store.DocRecord{
		"d1": {ID: "d1", Title: "T", Content: "# C"},
	}}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/docs/d1", nil)
	req.SetPathValue("id", "d1")
	docGetHandler(fs)(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("found doc: want 200, got %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/v1/docs/nope", nil)
	req.SetPathValue("id", "nope")
	docGetHandler(fs)(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("missing doc: want 404, got %d", rr.Code)
	}
}
