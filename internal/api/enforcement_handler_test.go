package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/adp/adp/internal/domain/enforcement"
)

type stubVerifier struct{ verified map[string]bool }

func (s stubVerifier) IsCommitVerified(_ context.Context, sha string) (bool, error) {
	return s.verified[sha], nil
}

func newRec(verified map[string]bool) *enforcement.Reconciler {
	return enforcement.NewReconciler(stubVerifier{verified}, enforcement.NewInMemoryFindingStore(0))
}

func TestObserveCommitsHandler_FlagsUngoverned(t *testing.T) {
	rec := newRec(map[string]bool{"a": true})
	rr := httptest.NewRecorder()
	body := `{"commits":[{"sha":"a","repo":"acme/x"},{"sha":"b","repo":"acme/x","ref":"refs/heads/main"}]}`
	observeCommitsHandler(rec)(rr, httptest.NewRequest(http.MethodPost, "/v1/enforcement/commits/observed", strings.NewReader(body)))
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}
	var resp struct {
		Observed int                   `json:"observed"`
		Flagged  int                   `json:"flagged"`
		Findings []enforcement.Finding `json:"findings"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Observed != 2 || resp.Flagged != 1 {
		t.Fatalf("unexpected: %+v", resp)
	}
	if resp.Findings[0].Reference != "b" || resp.Findings[0].Status != enforcement.StatusOpen {
		t.Fatalf("should flag ungoverned commit b: %+v", resp.Findings)
	}
}

func TestListAndResolveFinding(t *testing.T) {
	rec := newRec(map[string]bool{})
	observeCommitsHandler(rec)(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"commits":[{"sha":"b"}]}`)))

	lr := httptest.NewRecorder()
	listFindingsHandler(rec)(lr, httptest.NewRequest(http.MethodGet, "/v1/enforcement/findings?status=open", nil))
	var list struct {
		Items []enforcement.Finding `json:"items"`
		Total int                   `json:"total"`
	}
	json.Unmarshal(lr.Body.Bytes(), &list)
	if list.Total != 1 {
		t.Fatalf("want 1 open finding, got %d", list.Total)
	}
	id := list.Items[0].ID

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/v1/enforcement/findings/"+id, strings.NewReader(`{"status":"resolved"}`))
	req.SetPathValue("id", id)
	resolveFindingHandler(rec)(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("resolve want 200, got %d", rr.Code)
	}

	lr2 := httptest.NewRecorder()
	listFindingsHandler(rec)(lr2, httptest.NewRequest(http.MethodGet, "/v1/enforcement/findings?status=open", nil))
	var list2 struct {
		Total int `json:"total"`
	}
	json.Unmarshal(lr2.Body.Bytes(), &list2)
	if list2.Total != 0 {
		t.Fatalf("want 0 open after resolve, got %d", list2.Total)
	}
}

func TestEnforcementHandlers_ServiceUnavailableWhenNil(t *testing.T) {
	rr := httptest.NewRecorder()
	listFindingsHandler(nil)(rr, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", rr.Code)
	}
}

func TestResolveFinding_InvalidStatusAnd404(t *testing.T) {
	rec := newRec(map[string]bool{})

	rr := httptest.NewRecorder()
	bad := httptest.NewRequest(http.MethodPatch, "/x", strings.NewReader(`{"status":"bogus"}`))
	bad.SetPathValue("id", "whatever")
	resolveFindingHandler(rec)(rr, bad)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid status want 400, got %d", rr.Code)
	}

	rr2 := httptest.NewRecorder()
	nf := httptest.NewRequest(http.MethodPatch, "/x", strings.NewReader(`{"status":"resolved"}`))
	nf.SetPathValue("id", "nope")
	resolveFindingHandler(rec)(rr2, nf)
	if rr2.Code != http.StatusNotFound {
		t.Fatalf("not found want 404, got %d", rr2.Code)
	}
}
