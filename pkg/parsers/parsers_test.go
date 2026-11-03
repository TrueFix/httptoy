package parsers

import (
	"bytes"
	"crypto/tls"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestRequestMetadata(t *testing.T) {
	req := httptest.NewRequest("GET", "http://example.test/a%2Fb?x=1&x=2", nil)
	req.Header["X-Custom"] = []string{"one", "two"}
	req.Header.Set("X-Forwarded-Proto", "https")
	if got := ParseHeaders(req)["X-Custom"]; got != "one;two" {
		t.Fatalf("header = %q", got)
	}
	if got := ParseParams(req)["x"]; got != "1;2" {
		t.Fatalf("params = %q", got)
	}
	if got := ParseURL(req); got != "http://example.test/a%2Fb?x=1&x=2" {
		t.Fatalf("URL = %q", got)
	}
	req.TLS = &tls.ConnectionState{}
	if got := ParseURL(req); got != "https://example.test/a%2Fb?x=1&x=2" {
		t.Fatalf("TLS URL = %q", got)
	}
}

func TestParseBodyDataWithError(t *testing.T) {
	tests := []struct {
		name, ct, body string
		data           map[string]interface{}
		text           string
		wantError      bool
	}{
		{name: "JSON types", ct: "application/json", body: `{"number":2,"bool":true,"nil":null,"array":[1,"two"],"object":{"key":"value"}}`, data: map[string]interface{}{"number": float64(2), "bool": true, "nil": nil, "array": []interface{}{float64(1), "two"}, "object": map[string]interface{}{"key": "value"}}},
		{name: "invalid JSON", ct: "application/json", body: `{"a":`, wantError: true},
		{name: "multiple JSON values", ct: "application/json", body: `{} {}`, wantError: true},
		{name: "root array", ct: "application/json", body: `[]`, wantError: true},
		{name: "root null", ct: "application/json", body: `null`, wantError: true},
		{name: "form", ct: "application/x-www-form-urlencoded", body: "a=one&a=two&b=hello+world", data: map[string]interface{}{"a": "one;two", "b": "hello world"}},
		{name: "invalid form", ct: "application/x-www-form-urlencoded", body: "a=%XX", wantError: true},
		{name: "text with charset", ct: "text/plain; charset=utf-8", body: "hello", text: "hello", data: map[string]interface{}{}},
		{name: "unsupported", ct: "application/octet-stream", body: "hello", data: map[string]interface{}{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("DELETE", "/", strings.NewReader(tt.body))
			data, text, err := ParseBodyDataWithError(req, tt.ct)
			if (err != nil) != tt.wantError {
				t.Fatalf("error = %v", err)
			}
			if err == nil && (!reflect.DeepEqual(data, tt.data) || text != tt.text) {
				t.Fatalf("got %#v, %q", data, text)
			}
		})
	}
}

func TestLegacyBodyData(t *testing.T) {
	req := httptest.NewRequest("POST", "/", strings.NewReader(`{"hello":"world"}`))
	data, text := ParseBodyData(req, "application/json")
	if data["hello"] != "world" || text != "" {
		t.Fatalf("got %#v, %q", data, text)
	}
}

func TestMultipartCleanup(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if err := w.WriteField("name", "value"); err != nil {
		t.Fatal(err)
	}
	file, err := w.CreateFormFile("file", "test.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(file, strings.Repeat("x", 2000)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body.Bytes()))
	req.Header.Set("Content-Type", w.FormDataContentType())
	data, _, err := ParseBodyDataWithError(req, "multipart/form-data")
	if err != nil || data["name"] != "value" {
		t.Fatalf("data=%v error=%v", data, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary files left: %v, %v", entries, err)
	}
	// The unprivileged scratch image has no temporary directory. Bounded
	// multipart uploads must still work there without filesystem writes.
	t.Setenv("TMPDIR", dir+"/missing")
	req = httptest.NewRequest("POST", "/", bytes.NewReader(body.Bytes()))
	req.Header.Set("Content-Type", w.FormDataContentType())
	if _, _, err := ParseBodyDataWithError(req, "multipart/form-data"); err != nil {
		t.Fatalf("multipart requires temporary storage: %v", err)
	}
	t.Setenv("TMPDIR", dir)
	// A malformed ending must not leave temporary upload files behind.
	broken := bytes.TrimSuffix(body.Bytes(), []byte("--\r\n"))
	req = httptest.NewRequest("POST", "/", bytes.NewReader(broken))
	req.Header.Set("Content-Type", w.FormDataContentType())
	if _, _, err := ParseBodyDataWithError(req, "multipart/form-data"); err == nil {
		t.Fatal("expected malformed multipart error")
	}
	entries, err = os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary files left on error: %v, %v", entries, err)
	}
}

func TestBodyReadLimit(t *testing.T) {
	req := httptest.NewRequest("POST", "/", strings.NewReader("too long"))
	req.Body = http.MaxBytesReader(httptest.NewRecorder(), req.Body, 3)
	_, _, err := ParseBodyDataWithError(req, "application/octet-stream")
	var limitErr *http.MaxBytesError
	if !errors.As(err, &limitErr) {
		t.Fatalf("error = %v", err)
	}
}

func TestParseSupportedImage(t *testing.T) {
	for _, tt := range []struct{ accept, want string }{
		{"", ""},
		{"text/plain", ""},
		{"image/jpeg;q=0.9,image/png;q=0.1", "image/jpeg"},
		{"image/png;q=0,image/*;q=0.8", "image/jpeg"},
		{"image/*;q=0,*/*;q=1", ""},
		{"*/*", "image/png"},
		{"image/*", "image/png"},
		{"image/png;q=bogus,image/jpeg;q=0.5", "image/jpeg"},
		{"image/png;q=NaN,image/jpeg;q=0.5", "image/jpeg"},
		{"image/png;q=2", ""},
		{"image/png-extra", ""},
	} {
		t.Run(tt.accept, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.Header.Set("Accept", tt.accept)
			if got := ParseSupportedImage(req, []string{"image/png", "image/jpeg"}); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Add("Accept", "image/png;q=0.1")
	req.Header.Add("Accept", "image/jpeg;q=0.9")
	if got := ParseSupportedImage(req, []string{"image/png", "image/jpeg"}); got != "image/jpeg" {
		t.Fatalf("multiple headers: %q", got)
	}
}
