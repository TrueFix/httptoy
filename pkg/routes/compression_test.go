package routes

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/gorilla/mux"
)

func compressionRouter() *mux.Router {
	r := NewRouter()
	addAllCompression(r)
	return r
}

func TestCompressionRoutes(t *testing.T) {
	r := compressionRouter()
	for _, tc := range []struct {
		path, encoding string
	}{
		{"/gzip", "gzip"},
		{"/deflate", "deflate"},
		{"/zlib", "deflate"},
		{"/brotli", "br"},
		{"/utf8", ""},
	} {
		t.Run(tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", w.Code)
			}
			if got := w.Header().Get("Content-Encoding"); got != tc.encoding {
				t.Fatalf("Content-Encoding = %q, want %q", got, tc.encoding)
			}
			if got := w.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
				t.Errorf("Content-Type = %q", got)
			}
			var reader io.Reader = bytes.NewReader(w.Body.Bytes())
			switch tc.encoding {
			case "gzip":
				decoded, err := gzip.NewReader(reader)
				if err != nil {
					t.Fatal(err)
				}
				defer decoded.Close()
				reader = decoded
			case "deflate":
				// HTTP deflate requires the zlib wrapper, not raw DEFLATE.
				decoded, err := zlib.NewReader(reader)
				if err != nil {
					t.Fatal(err)
				}
				defer decoded.Close()
				reader = decoded
			case "br":
				reader = brotli.NewReader(reader)
			}
			body, err := io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}
			want := DEFAULT_STRING
			if tc.path == "/utf8" {
				want = DEFAULT_UNICODE
			}
			if string(body) != want {
				t.Errorf("decoded body = %q, want %q", body, want)
			}
		})
	}
}

func TestCompressionRoutesConcurrent(t *testing.T) {
	r := compressionRouter()
	var wg sync.WaitGroup
	for _, path := range []string{"/gzip", "/deflate", "/zlib", "/brotli", "/utf8"} {
		path := path
		baseline := httptest.NewRecorder()
		r.ServeHTTP(baseline, httptest.NewRequest(http.MethodGet, path, nil))
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 10; j++ {
					w := httptest.NewRecorder()
					r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
					if w.Code != baseline.Code || !bytes.Equal(w.Body.Bytes(), baseline.Body.Bytes()) {
						t.Errorf("%s: concurrent response differs", path)
						return
					}
				}
			}()
		}
	}
	wg.Wait()
}

func BenchmarkCompressionRoutes(b *testing.B) {
	r := compressionRouter()
	for _, path := range []string{"/gzip", "/deflate", "/zlib", "/brotli", "/utf8"} {
		b.Run(path, func(b *testing.B) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
			}
		})
	}
}
