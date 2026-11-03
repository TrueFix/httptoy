package server

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestEmbeddedServer(t *testing.T) {
	t.Setenv("TRUSTED_PROXIES", "")
	t.Chdir(t.TempDir())
	r := Server()
	for _, path := range []string{"/", "/favicon.ico", "/image/jpeg", "/json"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || w.Body.Len() == 0 {
			t.Fatalf("%s: status=%d size=%d", path, w.Code, w.Body.Len())
		}
	}
}

func TestServerOpenAPIIncludesUtilityRoutes(t *testing.T) {
	t.Setenv("TRUSTED_PROXIES", "")
	w := httptest.NewRecorder()
	Server().ServeHTTP(w, httptest.NewRequest("GET", "/openapi.json", nil))
	var spec struct {
		Paths map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/version", "/favicon.ico", "/sessions", "/sse/resume", "/openapi.json", "/metrics"} {
		if spec.Paths[path] == nil {
			t.Fatalf("OpenAPI omits %s", path)
		}
	}
}

func TestProxyTrust(t *testing.T) {
	for _, proxies := range []string{"", "192.0.2.0/24"} {
		t.Run(proxies, func(t *testing.T) {
			t.Setenv("TRUSTED_PROXIES", proxies)
			r := Server()
			req := httptest.NewRequest("GET", "/ip", nil)
			req.RemoteAddr = "192.0.2.1:1234"
			req.Header.Set("X-Forwarded-For", "203.0.113.1")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			want := "192.0.2.1"
			if proxies != "" {
				want = "203.0.113.1"
			}
			if w.Body.String() != want {
				t.Fatalf("IP=%q want=%q", w.Body.String(), want)
			}
		})
	}
}

func TestHTTPTimeouts(t *testing.T) {
	t.Setenv("TRUSTED_PROXIES", "")
	srv := HTTPServer(":8000")
	if srv.ReadHeaderTimeout <= 0 || srv.ReadTimeout <= 0 || srv.WriteTimeout <= 10e9 || srv.IdleTimeout <= 0 {
		t.Fatal("missing or incompatible server deadlines")
	}
}
