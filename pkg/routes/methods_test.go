package routes

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
)

func methodsTestRouter() *mux.Router {
	r := NewRouter()
	addAllMethods(r)
	return r
}

func TestMethodBodyRoutes(t *testing.T) {
	r := methodsTestRouter()
	for _, tt := range []struct{ method, path string }{
		{"POST", "/post"}, {"PUT", "/put"}, {"DELETE", "/delete"}, {"PATCH", "/patch"},
	} {
		t.Run(tt.method, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(`{"count":3,"items":[true,null]}`))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status %d: %s", rec.Code, rec.Body)
			}
			var payload method_with_body_payload
			if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if payload.Data["count"] != float64(3) {
				t.Fatalf("data = %#v", payload.Data)
			}
		})
	}
}

func TestMethodBodyErrorsAndLimits(t *testing.T) {
	r := methodsTestRouter()
	var multipartBody bytes.Buffer
	w := multipart.NewWriter(&multipartBody)
	if err := w.WriteField("field", strings.Repeat("x", (1<<20)+1)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, ct, body string
		chunked        bool
		status         int
	}{
		{"bad JSON", "application/json", "{", false, 400},
		{"bad form", "application/x-www-form-urlencoded", "a=%ZZ", false, 400},
		{"bad multipart", "multipart/form-data", "broken", false, 400},
		{"large text", "text/plain", strings.Repeat("x", (1<<20)+1), false, 413},
		{"chunked text", "text/plain", strings.Repeat("x", (1<<20)+1), true, 413},
		{"large unknown", "application/octet-stream", strings.Repeat("x", (1<<20)+1), true, 413},
		{"large no type", "", strings.Repeat("x", (1<<20)+1), true, 413},
		{"large form", "application/x-www-form-urlencoded", "a=" + strings.Repeat("x", 1<<20), true, 413},
		{"large JSON", "application/json", `{"a":"` + strings.Repeat("x", 1<<20) + `"}`, true, 413},
		{"large multipart", w.FormDataContentType(), multipartBody.String(), true, 413},
		{"at limit", "text/plain", strings.Repeat("x", 1<<20), false, 200},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/post", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", tt.ct)
			if tt.chunked {
				req.ContentLength = -1
				req.TransferEncoding = []string{"chunked"}
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d", rec.Code, tt.status)
			}
		})
	}
}

func TestGetRequestSummary(t *testing.T) {
	req := httptest.NewRequest("GET", "http://example.test/get?a=1&a=2", nil)
	req.Header.Set("X-Custom", "present")
	rec := httptest.NewRecorder()
	methodsTestRouter().ServeHTTP(rec, req)
	var payload method_get_payload
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 200 || payload.Args["a"] != "1;2" || payload.Headers["X-Custom"] != "present" || payload.Url != "http://example.test/get?a=1&a=2" {
		t.Fatalf("status=%d payload=%+v", rec.Code, payload)
	}
}
