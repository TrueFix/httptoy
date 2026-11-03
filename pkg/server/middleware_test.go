package server

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestPanicRecovery(t *testing.T) {
	w := httptest.NewRecorder()
	requestMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("failure") })).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 500 {
		t.Fatalf("panic status=%d", w.Code)
	}
	func() {
		defer func() {
			if got := recover(); got != http.ErrAbortHandler {
				t.Fatalf("partial response panic=%v", got)
			}
		}()
		requestMiddleware(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { _, _ = w.Write([]byte("partial")); panic("failure") })).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	}()
}

func TestSensitiveLogPaths(t *testing.T) {
	for _, target := range []string{"/basic/user/secret?token=secret", "/%62asic/user/secret", "/anything/private-data?token=secret", "/body/secret", "/cookies/set/session/secret", "/sessions/random-id/capture?token=secret"} {
		path := safeLogPath(httptest.NewRequest("GET", target, nil))
		if strings.Contains(path, "secret") || strings.Contains(path, "private-data") || strings.Contains(path, "random-id") || strings.Contains(path, "?") {
			t.Fatalf("sensitive request target logged: %s", path)
		}
	}
	if got := safeLogPath(httptest.NewRequest("GET", "/healthz?password=secret", nil)); got != "/healthz" {
		t.Fatal(got)
	}
	for _, target := range []string{"/sessions/id/%0AFORGED", "/sessions/id/%0DFORGED", "/sessions/id/%1B%5B2J"} {
		if logged := safeLogPath(httptest.NewRequest("GET", target, nil)); strings.ContainsAny(logged, "\r\n\x1b") {
			t.Fatalf("log control characters were not escaped: %q", logged)
		}
	}
}

type controllerWriter struct {
	*httptest.ResponseRecorder
	deadline   time.Time
	flushError error
}

func (w *controllerWriter) SetWriteDeadline(deadline time.Time) error {
	w.deadline = deadline
	return nil
}
func (w *controllerWriter) FlushError() error { return w.flushError }

func TestResponseController(t *testing.T) {
	w := &controllerWriter{ResponseRecorder: httptest.NewRecorder(), flushError: errors.New("flush failed")}
	requestMiddleware(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		controller := http.NewResponseController(w)
		if err := controller.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		if err := controller.Flush(); err == nil || err.Error() != "flush failed" {
			t.Fatalf("flush error=%v", err)
		}
		if _, _, err := controller.Hijack(); !errors.Is(err, http.ErrNotSupported) {
			t.Fatalf("hijack error=%v", err)
		}
	})).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.deadline.IsZero() {
		t.Fatal("write deadline was swallowed by middleware")
	}
}

func TestMiddlewareStreamingProtocols(t *testing.T) {
	t.Setenv("TRUSTED_PROXIES", "")
	for _, http2 := range []bool{false, true} {
		t.Run(map[bool]string{false: "HTTP1", true: "HTTP2"}[http2], func(t *testing.T) {
			srv := httptest.NewUnstartedServer(Server())
			srv.EnableHTTP2 = http2
			if http2 {
				srv.StartTLS()
			} else {
				srv.Start()
			}
			defer srv.Close()
			resp, err := srv.Client().Get(srv.URL + "/trailers")
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil || string(body) != "i love httptoy" || resp.Trailer.Get("X-Stream-Complete") != "true" {
				t.Fatalf("trailers: %q %v %v", body, resp.Trailer, err)
			}
			if http2 && resp.ProtoMajor != 2 {
				t.Fatal("HTTP/2 not exercised")
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, "GET", srv.URL+"/sse?count=2&interval_ms=1000", nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err = srv.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			line, err := bufio.NewReader(resp.Body).ReadString('\n')
			resp.Body.Close()
			cancel()
			if err != nil || line != "id: 0\n" {
				t.Fatalf("SSE flush: %q %v", line, err)
			}
		})
	}
}

func TestMiddlewareWebSocket(t *testing.T) {
	t.Setenv("TRUSTED_PROXIES", "")
	srv := httptest.NewServer(Server())
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	if err := conn.Write(ctx, websocket.MessageText, []byte("echo")); err != nil {
		t.Fatal(err)
	}
	kind, data, err := conn.Read(ctx)
	if err != nil || kind != websocket.MessageText || string(data) != "echo" {
		t.Fatalf("WebSocket: %v %q %v", kind, data, err)
	}
	conn.Close(websocket.StatusNormalClosure, "")
}
