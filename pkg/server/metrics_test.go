package server

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/TrueFix/httptoy/pkg/routes"
	"github.com/coder/websocket"
)

func testMetrics(h http.Handler) string {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	return w.Body.String()
}

func metricNumber(t *testing.T, text, name string) float64 {
	t.Helper()
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, name+" ") {
			value, err := strconv.ParseFloat(strings.TrimPrefix(line, name+" "), 64)
			if err != nil {
				t.Fatal(err)
			}
			return value
		}
	}
	t.Fatalf("missing metric %s", name)
	return 0
}

func TestServerMetricsBoundedRouteLabels(t *testing.T) {
	t.Setenv("TRUSTED_PROXIES", "")
	t.Setenv("APP_VERSION", "1.0.0")
	t.Setenv("GIT_SHA", "testsha")
	h := Server()
	for _, tc := range []struct{ method, path string }{
		{"GET", "/get"}, {"GET", "/get"}, {"GET", "/status/418"},
		{"GET", "/anything/customer-secret?private=secret-query"},
		{"GET", "/sessions/private-session-id/requests"},
		{"CUSTOM-SECRET", "/unknown-secret"}, {"GET", "/not-found-secret"},
	} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(tc.method, tc.path, nil))
	}
	text := testMetrics(h)
	for _, secret := range []string{"customer-secret", "secret-query", "private-session-id", "CUSTOM-SECRET", "unknown-secret", "not-found-secret"} {
		if strings.Contains(text, secret) {
			t.Fatalf("metric leaked %q", secret)
		}
	}
	for _, sample := range []string{
		`httptoy_info{version="1.0.0",git_sha="testsha"} 1`,
		`httptoy_http_requests_total{method="GET",route="/get",status="200"} 2`,
		`httptoy_http_requests_total{method="GET",route="/status/{code}",status="418"} 1`,
		`httptoy_http_requests_total{method="GET",route="/anything/{path}",status="200"} 1`,
		`httptoy_http_requests_total{method="GET",route="/sessions/{id}/requests",status="401"} 1`,
		`httptoy_http_requests_total{method="OTHER",route="unmatched",status="404"} 1`,
	} {
		if !strings.Contains(text, sample+"\n") {
			t.Fatalf("missing %s", sample)
		}
	}
	if metricNumber(t, text, "httptoy_http_requests_in_flight") != 1 {
		t.Fatal("scrape itself must be in flight")
	}
	text = testMetrics(h)
	if !strings.Contains(text, `httptoy_http_requests_total{method="GET",route="/metrics",status="200"} 1`) {
		t.Fatal("completed scrape not counted")
	}
}

func TestMetricsAggregateSessionResources(t *testing.T) {
	t.Setenv("TRUSTED_PROXIES", "")
	h := Server()
	baseline := routes.ResourceSnapshot()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/sessions", strings.NewReader("{}")))
	var session struct{ ID, Token string }
	if err := json.Unmarshal(w.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if w.Code != 201 {
		t.Fatal(w.Code)
	}
	request := func(method, action, body string, headers map[string]string) {
		t.Helper()
		req := httptest.NewRequest(method, "/sessions/"+session.ID+action, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+session.Token)
		for key, value := range headers {
			req.Header.Set(key, value)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code >= 400 {
			t.Fatalf("session request: %d %s", w.Code, w.Body)
		}
	}
	t.Cleanup(func() { request("DELETE", "", "", nil) })
	request("POST", "/capture", "secret payload", nil)
	request("POST", "/idempotency", "secret payload", map[string]string{"Idempotency-Key": "private-key"})
	text := testMetrics(h)
	if strings.Contains(text, session.ID) || strings.Contains(text, session.Token) || strings.Contains(text, "secret payload") || strings.Contains(text, "private-key") {
		t.Fatal("session metrics exposed data")
	}
	if metricNumber(t, text, "httptoy_sessions_active") != float64(baseline.SessionsActive+1) || metricNumber(t, text, "httptoy_session_captured_requests") != float64(baseline.CapturedRequests+1) || metricNumber(t, text, "httptoy_session_idempotency_keys") != float64(baseline.IdempotencyKeys+1) || metricNumber(t, text, "httptoy_session_capture_bytes") <= float64(baseline.CaptureBytes) {
		t.Fatal("session gauges do not reflect live state")
	}
}

func TestMetricsStreamLifetimeAndCancellation(t *testing.T) {
	t.Setenv("TRUSTED_PROXIES", "")
	h := Server()
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/sse" {
			defer close(done)
		}
		h.ServeHTTP(w, req)
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", srv.URL+"/sse?count=10&interval_ms=500", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if _, err := bufio.NewReader(response.Body).ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	text := testMetrics(h)
	if metricNumber(t, text, "httptoy_sse_clients") < 1 || metricNumber(t, text, "httptoy_http_requests_in_flight") < 2 {
		t.Fatal("active stream not included in gauges")
	}
	cancel()
	response.Body.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stream did not end")
	}
	text = testMetrics(h)
	if metricNumber(t, text, "httptoy_http_requests_in_flight") != 1 || metricNumber(t, text, "httptoy_http_aborted_requests_total") != 1 || !strings.Contains(text, `httptoy_http_requests_total{method="GET",route="/sse",status="200"} 1`) {
		t.Fatal("canceled stream did not complete its metrics")
	}
}

func TestMetricsRecoveryAndIntentionalAbort(t *testing.T) {
	r := routes.NewRouter()
	r.HandleFunc("/panic", func(http.ResponseWriter, *http.Request) { panic("test panic") }).Methods("GET")
	r.HandleFunc("/abort", func(w http.ResponseWriter, req *http.Request) {
		_, _ = io.WriteString(w, "partial")
		panic(http.ErrAbortHandler)
	}).Methods("GET")
	meter := instrumentRouter(r)
	h := requestMiddlewareWithMetrics(r, meter)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/panic", nil))
	if w.Code != 500 {
		t.Fatal(w.Code)
	}
	func() {
		defer func() {
			if recovered := recover(); recovered != http.ErrAbortHandler {
				t.Fatalf("abort panic=%v", recovered)
			}
		}()
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/abort", nil))
	}()
	text := testMetrics(h)
	if metricNumber(t, text, "httptoy_http_panics_total") != 1 || metricNumber(t, text, "httptoy_http_aborted_requests_total") != 1 || metricNumber(t, text, "httptoy_http_requests_in_flight") != 1 {
		t.Fatal("recovery metrics are incorrect")
	}
	if !strings.Contains(text, `httptoy_http_requests_total{method="GET",route="/panic",status="500"} 1`) {
		t.Fatal("panic status not counted")
	}
}

func TestMetricsWebSocketLifetime(t *testing.T) {
	t.Setenv("TRUSTED_PROXIES", "")
	h := Server()
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/ws" {
			defer close(done)
		}
		h.ServeHTTP(w, req)
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	text := testMetrics(h)
	if metricNumber(t, text, `httptoy_resource_pool_active{pool="websockets"}`) < 1 || metricNumber(t, text, "httptoy_http_requests_in_flight") < 2 {
		t.Fatal("active WebSocket not counted")
	}
	conn.Close(websocket.StatusNormalClosure, "")
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("WebSocket handler did not complete")
	}
	text = testMetrics(h)
	if !strings.Contains(text, `httptoy_http_requests_total{method="GET",route="/ws",status="101"} 1`) {
		t.Fatal("WebSocket completion status not counted")
	}
	if metricNumber(t, text, "httptoy_http_requests_in_flight") != 1 {
		t.Fatal("WebSocket in-flight count leaked")
	}
}
