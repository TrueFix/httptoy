package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestWebsitePrerenderAndMetadata(t *testing.T) {
	t.Setenv("PUBLIC_URL", "https://tests.example")
	h := Server()
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "http://untrusted.example/", nil)
	h.ServeHTTP(w, req)
	body := w.Body.String()
	for _, want := range []string{`<h1`, `BREAK THE`, `HAPPY PATH.`, `class="endpoint"`, `/sessions/{id}/idempotency`, `href="https://tests.example/"`, `https://tests.example/assets/social.png`, `<!-- -->`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing pre-rendered content %q", want)
		}
	}
	if w.Code != 200 || w.Header().Get("X-Robots-Tag") != "" || w.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("homepage headers: %d %v", w.Code, w.Header())
	}
	if strings.Contains(body, "untrusted.example") || strings.Contains(body, "{{.") {
		t.Fatal("untrusted host or unresolved template in homepage")
	}
	schemas := regexp.MustCompile(`<script type="application/ld\+json">(.*?)</script>`).FindAllStringSubmatch(body, -1)
	if len(schemas) != 2 {
		t.Fatalf("got %d structured data blocks", len(schemas))
	}
	for _, schema := range schemas {
		if !json.Valid([]byte(schema[1])) {
			t.Errorf("invalid structured data: %s", schema[1])
		}
	}
	assets := regexp.MustCompile(`(?:src|href)="(/assets/[^" ]+)"`).FindAllStringSubmatch(body, -1)
	if len(assets) < 4 {
		t.Fatal("missing asset references")
	}
	for _, asset := range assets {
		result := httptest.NewRecorder()
		h.ServeHTTP(result, httptest.NewRequest("GET", asset[1], nil))
		if result.Code != 200 {
			t.Errorf("asset %s: %d", asset[1], result.Code)
		}
		if hashedAsset.MatchString(asset[1]) && !strings.Contains(result.Header().Get("Cache-Control"), "immutable") {
			t.Errorf("unhashed caching for %s", asset[1])
		}
	}
}

func TestWebsiteDiscoveryAndAPIIndexing(t *testing.T) {
	t.Setenv("PUBLIC_URL", "https://tests.example/")
	h := Server()
	for _, tc := range []struct {
		path, content string
		noindex       bool
	}{
		{"/robots.txt", "Sitemap: https://tests.example/sitemap.xml", false},
		{"/sitemap.xml", "<loc>https://tests.example/</loc>", false},
		{"/anything", `"method"`, true},
		{"/openapi.json", `"openapi"`, true},
		{"/assets/licenses.txt", "Frontend third-party notices", false},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
		if w.Code != 200 || !strings.Contains(w.Body.String(), tc.content) {
			t.Errorf("%s: %d, missing %q", tc.path, w.Code, tc.content)
		}
		if got := w.Header().Get("X-Robots-Tag") != ""; got != tc.noindex {
			t.Errorf("%s noindex=%v", tc.path, got)
		}
	}
	for _, path := range []string{"/", "/sitemap.xml", "/assets/favicon.svg"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodHead, path, nil))
		if w.Code != 200 {
			t.Errorf("HEAD %s: %d", path, w.Code)
		}
	}
	for _, path := range []string{"/assets/", "/assets/missing.js"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 404 {
			t.Errorf("%s: %d", path, w.Code)
		}
	}
}

func TestPublicURLValidation(t *testing.T) {
	for _, value := range []string{"//evil.example", "javascript:alert(1)", "https://user:pass@example.com", "https://example.com/path", "https://example.com?q=x", "https://example.com/#x"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("PUBLIC_URL", value)
			defer func() {
				if recover() == nil {
					t.Error("expected invalid origin to panic")
				}
			}()
			publicURL()
		})
	}
	t.Setenv("PUBLIC_URL", "")
	if publicURL() != "https://httptoy.com" {
		t.Fatal("unexpected default origin")
	}
}
