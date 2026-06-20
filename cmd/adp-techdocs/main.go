// Command adp-techdocs exports ADP-generated documentation as MkDocs source
// trees, one per Backstage entity, ready for `techdocs-cli generate` and
// `techdocs-cli publish` (the Backstage TechDocs "external" builder).
//
// It reads from the ADP REST API (adp-server): services, generated docs, and
// the session->service mapping. It writes nothing to your TechDocs storage —
// that is done by techdocs-cli (see scripts/techdocs-publish.sh), which this
// command prints ready-to-run commands for.
//
// Usage:
//
//	ADP_URL=http://localhost:8080 ADP_API_KEY=... \
//	  adp-techdocs --out ./techdocs-out --publisher awsS3 --bucket my-techdocs
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/adp/adp/internal/techdocs"
)

type apiClient struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func (c *apiClient) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

type servicesResp struct {
	Items []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"items"`
}

type docsResp struct {
	Items []struct {
		ID        string `json:"id"`
		SessionID string `json:"session_id"`
		Category  string `json:"category"`
		Title     string `json:"title"`
		Content   string `json:"content"`
		CreatedAt string `json:"created_at"`
	} `json:"items"`
}

type sessionResp struct {
	Data struct {
		ServiceScope []string `json:"service_scope"`
	} `json:"data"`
}

func main() {
	baseURL := flag.String("url", env("ADP_URL", "http://localhost:8080"), "ADP server base URL")
	outDir := flag.String("out", "./techdocs-out", "output directory for MkDocs source trees")
	namespace := flag.String("namespace", "default", "Backstage entity namespace")
	kind := flag.String("kind", "Component", "Backstage entity kind")
	limit := flag.Int("limit", 200, "max items per ADP list call")
	publisher := flag.String("publisher", "awsS3", "techdocs-cli publisher type for the printed publish commands")
	bucket := flag.String("bucket", "<your-techdocs-bucket>", "TechDocs storage bucket/container for the printed publish commands")
	flag.Parse()

	c := &apiClient{
		baseURL: strings.TrimRight(*baseURL, "/"),
		apiKey:  os.Getenv("ADP_API_KEY"),
		http:    &http.Client{Timeout: 30 * time.Second},
	}
	ctx := context.Background()

	// 1. Services -> entity targets.
	var svc servicesResp
	if err := c.get(ctx, fmt.Sprintf("/v1/services?limit=%d", *limit), &svc); err != nil {
		fatalf("list services: %v", err)
	}
	services := make([]techdocs.Service, 0, len(svc.Items))
	for _, s := range svc.Items {
		services = append(services, techdocs.Service{ID: s.ID, Name: s.Name, Namespace: *namespace, Kind: *kind})
	}
	if len(services) == 0 {
		fmt.Fprintln(os.Stderr, "no services found; nothing to publish")
		return
	}

	// 2. Generated docs across categories.
	var docs []techdocs.Doc
	sessionIDs := map[string]bool{}
	for _, cat := range []string{"session_summary", "risk_report", "pattern_report"} {
		var dr docsResp
		if err := c.get(ctx, fmt.Sprintf("/v1/docs?category=%s&limit=%d", cat, *limit), &dr); err != nil {
			fatalf("list docs (%s): %v — is adp-server running with a DocStore (SQLite mode)?", cat, err)
		}
		for _, d := range dr.Items {
			created, _ := time.Parse(time.RFC3339, d.CreatedAt)
			docs = append(docs, techdocs.Doc{
				ID: d.ID, SessionID: d.SessionID, Category: d.Category,
				Title: d.Title, Content: d.Content, CreatedAt: created,
			})
			if d.SessionID != "" {
				sessionIDs[d.SessionID] = true
			}
		}
	}

	// 3. session -> service mapping (from each session's service_scope).
	sessionServices := map[string][]string{}
	for sid := range sessionIDs {
		var sr sessionResp
		if err := c.get(ctx, "/v1/sessions/"+sid, &sr); err != nil {
			fmt.Fprintf(os.Stderr, "warning: session %s lookup failed (%v); its docs are skipped\n", sid, err)
			continue
		}
		sessionServices[sid] = sr.Data.ServiceScope
	}

	// 4. Group docs onto entities and write MkDocs source trees.
	sites := techdocs.GroupDocsByEntity(services, sessionServices, docs)
	written := 0
	fmt.Println("# Run these (or use scripts/techdocs-publish.sh):")
	for _, site := range sites {
		if len(site.Docs) == 0 {
			continue
		}
		dir, err := techdocs.WriteSite(*outDir, site)
		if err != nil {
			fatalf("write site %s: %v", site.EntityRef(), err)
		}
		written++
		fmt.Printf("techdocs-cli generate --no-docker --source-dir %q --output-dir %q && \\\n", dir, dir+"/site")
		fmt.Printf("techdocs-cli publish --publisher-type %s --storage-name %s --entity %s --directory %q\n\n",
			*publisher, *bucket, site.EntityRef(), dir+"/site")
	}
	fmt.Fprintf(os.Stderr, "wrote %d entity doc site(s) under %s\n", written, *outDir)
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func fatalf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "adp-techdocs: "+format+"\n", a...)
	os.Exit(1)
}
