package techdocs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSanitizeName(t *testing.T) {
	cases := map[string]string{
		"My Service!":   "my-service",
		"payments-api":  "payments-api",
		"Foo  Bar__Baz": "foo-bar-baz",
		"--leading--":   "leading",
	}
	for in, want := range cases {
		if got := SanitizeName(in); got != want {
			t.Errorf("SanitizeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildSite(t *testing.T) {
	now := time.Now()
	site := EntitySite{
		Namespace: "default",
		Kind:      "Component",
		Name:      "payments-api",
		Title:     "Payments API",
		Docs: []Doc{
			{ID: "d1", SessionID: "s1", Category: "session_summary", Title: "Session s1 summary", Content: "# Summary\nbody", CreatedAt: now},
			{ID: "d2", SessionID: "s1", Category: "risk_report", Title: "Risk: prod write", Content: "risky", CreatedAt: now},
		},
	}

	files := BuildSite(site)

	mk, ok := files["mkdocs.yml"]
	if !ok {
		t.Fatal("missing mkdocs.yml")
	}
	if !strings.Contains(mk, "techdocs-core") {
		t.Error("mkdocs.yml must enable the techdocs-core plugin")
	}
	if !strings.Contains(mk, "site_name:") || !strings.Contains(mk, "Payments API") {
		t.Error("mkdocs.yml must set a site_name from the title")
	}
	if !strings.Contains(mk, "nav:") || !strings.Contains(mk, "Overview: index.md") {
		t.Error("mkdocs.yml must include a nav with the overview")
	}

	if _, ok := files["docs/index.md"]; !ok {
		t.Fatal("missing docs/index.md")
	}
	if _, ok := files["docs/session_summary/session-s1-summary.md"]; !ok {
		t.Fatalf("missing expected session_summary page; got files: %v", keys(files))
	}
	if _, ok := files["docs/risk_report/risk-prod-write.md"]; !ok {
		t.Fatalf("missing expected risk_report page; got files: %v", keys(files))
	}
	// doc page should embed the markdown content and session metadata
	page := files["docs/session_summary/session-s1-summary.md"]
	if !strings.Contains(page, "# Summary") || !strings.Contains(page, "Session: `s1`") {
		t.Errorf("doc page missing content/metadata: %q", page)
	}
}

func TestBuildSite_SlugCollision(t *testing.T) {
	site := EntitySite{
		Namespace: "default", Kind: "Component", Name: "svc", Title: "Svc",
		Docs: []Doc{
			{ID: "a", Category: "session_summary", Title: "Same Title", Content: "x"},
			{ID: "b", Category: "session_summary", Title: "Same Title", Content: "y"},
		},
	}
	files := BuildSite(site)
	if _, ok := files["docs/session_summary/same-title.md"]; !ok {
		t.Errorf("missing first slug; got %v", keys(files))
	}
	if _, ok := files["docs/session_summary/same-title-2.md"]; !ok {
		t.Errorf("missing de-duped slug; got %v", keys(files))
	}
}

func TestGroupDocsByEntity(t *testing.T) {
	services := []Service{
		{ID: "svc-1", Name: "Payments API"},
		{ID: "svc-2", Name: "Ledger"},
	}
	sessionServices := map[string][]string{
		"s1": {"svc-1"},
		"s2": {"svc-1", "svc-2"},
		"s3": {"unknown"},
	}
	docs := []Doc{
		{ID: "d1", SessionID: "s1", Category: "session_summary", Title: "A"},
		{ID: "d2", SessionID: "s2", Category: "risk_report", Title: "B"},
		{ID: "d3", SessionID: "s3", Category: "session_summary", Title: "C"}, // dropped (unknown service)
	}

	sites := GroupDocsByEntity(services, sessionServices, docs)
	if len(sites) != 2 {
		t.Fatalf("want 2 sites, got %d", len(sites))
	}
	if sites[0].Name != "payments-api" || len(sites[0].Docs) != 2 {
		t.Errorf("svc-1 should have d1+d2: %+v", sites[0])
	}
	if sites[1].Name != "ledger" || len(sites[1].Docs) != 1 || sites[1].Docs[0].ID != "d2" {
		t.Errorf("svc-2 should have only d2: %+v", sites[1])
	}
	if sites[0].EntityRef() != "default/Component/payments-api" {
		t.Errorf("unexpected entity ref %q", sites[0].EntityRef())
	}
}

func TestWriteSite(t *testing.T) {
	dir := t.TempDir()
	site := EntitySite{
		Namespace: "default", Kind: "Component", Name: "svc", Title: "Svc",
		Docs: []Doc{{ID: "d1", Category: "session_summary", Title: "T", Content: "c"}},
	}
	out, err := WriteSite(dir, site)
	if err != nil {
		t.Fatalf("WriteSite: %v", err)
	}
	if out != filepath.Join(dir, "default", "Component", "svc") {
		t.Errorf("unexpected out dir %q", out)
	}
	if _, err := os.Stat(filepath.Join(out, "mkdocs.yml")); err != nil {
		t.Errorf("mkdocs.yml not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "docs", "index.md")); err != nil {
		t.Errorf("docs/index.md not written: %v", err)
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
