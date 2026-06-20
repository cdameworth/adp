package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func postContext(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/context", strings.NewReader(body))
	rr := httptest.NewRecorder()
	contextHandler(rr, req)
	return rr
}

func TestContextHandler_Validation(t *testing.T) {
	if rr := postContext(t, `{}`); rr.Code != http.StatusBadRequest {
		t.Fatalf("missing session_id: want 400, got %d", rr.Code)
	}
	if rr := postContext(t, `{"session_id":"s1"}`); rr.Code != http.StatusBadRequest {
		t.Fatalf("missing task: want 400, got %d", rr.Code)
	}
	if rr := postContext(t, `not json`); rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid body: want 400, got %d", rr.Code)
	}
}

func TestContextHandler_AssemblesRequestScopedContext(t *testing.T) {
	rr := postContext(t, `{"session_id":"sess-123","service_id":"svc-9","task":"refactor auth"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}

	var resp struct {
		Data struct {
			SessionID string `json:"session_id"`
			ServiceID string `json:"service_id"`
			Task      string `json:"task"`
			Layers    map[string]struct {
				Content string   `json:"content"`
				Tokens  int      `json:"tokens"`
				Sources []string `json:"sources"`
			} `json:"layers"`
			TotalTokens int  `json:"total_tokens"`
			CacheHit    bool `json:"cache_hit"`
			Degraded    bool `json:"degraded"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	d := resp.Data

	if d.SessionID != "sess-123" || d.ServiceID != "svc-9" || d.Task != "refactor auth" {
		t.Fatalf("request not echoed back: %+v", d)
	}
	ess, ok := d.Layers["essential"]
	if !ok || !strings.Contains(ess.Content, "sess-123") {
		t.Fatalf("essential layer should reference the session: %+v", ess)
	}
	if ess.Tokens <= 0 {
		t.Fatalf("essential tokens should be > 0, got %d", ess.Tokens)
	}
	if !strings.Contains(d.Layers["task_relevant"].Content, "refactor auth") {
		t.Fatalf("task_relevant layer should contain the task")
	}
	sum := d.Layers["essential"].Tokens + d.Layers["task_relevant"].Tokens + d.Layers["supporting"].Tokens
	if d.TotalTokens != sum {
		t.Fatalf("total_tokens %d != sum of layers %d", d.TotalTokens, sum)
	}
	if d.CacheHit {
		t.Fatalf("cache_hit should be false")
	}
	if !d.Degraded {
		t.Fatalf("degraded should be true for the lightweight assembler")
	}
}

func TestEstimateTokens(t *testing.T) {
	cases := map[string]int{"": 0, "abcd": 1, "abcde": 2}
	for in, want := range cases {
		if got := estimateTokens(in); got != want {
			t.Fatalf("estimateTokens(%q): want %d, got %d", in, want, got)
		}
	}
}
