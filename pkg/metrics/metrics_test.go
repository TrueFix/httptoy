package metrics

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func scrape(r *Registry) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	return w
}

func TestPrometheusExposition(t *testing.T) {
	r := New("1.0.0", "abc123", []string{"/sessions/{id}/capture"}, func() Resources {
		return Resources{Pools: []Pool{{Name: "streams", Active: 2, Capacity: 128, Rejected: 3}}, SessionsActive: 1, CaptureBytes: 12, CapturedRequests: 2, IdempotencyKeys: 3, SSEClients: 1}
	})
	r.Begin()
	r.Complete("POST", "/sessions/{id}/capture", 201, 12*time.Millisecond, false, false)
	w := scrape(r)
	text := w.Body.String()
	if w.Code != 200 || w.Header().Get("Content-Type") != "text/plain; version=0.0.4; charset=utf-8" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("metrics headers=%v", w.Header())
	}
	for _, sample := range []string{
		`# HELP httptoy_info httptoy build information.`,
		`# TYPE httptoy_info gauge`,
		`httptoy_info{version="1.0.0",git_sha="abc123"} 1`,
		`# TYPE httptoy_http_requests_total counter`,
		`httptoy_http_requests_total{method="POST",route="/sessions/{id}/capture",status="201"} 1`,
		`# TYPE httptoy_http_request_duration_seconds histogram`,
		`httptoy_http_request_duration_seconds_bucket{method="POST",route="/sessions/{id}/capture",le="0.01"} 0`,
		`httptoy_http_request_duration_seconds_bucket{method="POST",route="/sessions/{id}/capture",le="0.025"} 1`,
		`httptoy_http_request_duration_seconds_bucket{method="POST",route="/sessions/{id}/capture",le="+Inf"} 1`,
		`httptoy_http_request_duration_seconds_sum{method="POST",route="/sessions/{id}/capture"} 0.012`,
		`httptoy_http_request_duration_seconds_count{method="POST",route="/sessions/{id}/capture"} 1`,
		`httptoy_http_requests_in_flight 0`,
		`httptoy_resource_pool_active{pool="streams"} 2`,
		`httptoy_resource_pool_capacity{pool="streams"} 128`,
		`httptoy_admission_rejections_total{pool="streams"} 3`,
		`httptoy_admission_rejections_total{pool="metrics"} 0`,
		`httptoy_sessions_active 1`,
		`httptoy_session_capture_bytes 12`,
		`httptoy_session_captured_requests 2`,
		`httptoy_session_idempotency_keys 3`,
		`httptoy_sse_clients 1`,
	} {
		if !strings.Contains(text, sample+"\n") {
			t.Fatalf("missing sample %q", sample)
		}
	}
}

func TestBoundedLabelsAndEscaping(t *testing.T) {
	r := New("v\"\\\n\x1b", "", []string{"/anything/{path:.*}"}, nil)
	for i := 0; i < 100; i++ {
		r.Begin()
		r.Complete(fmt.Sprintf("UNTRUSTED-%d", i), fmt.Sprintf("/unknown/user-%d?secret=token", i), 9000+i, time.Millisecond, true, true)
	}
	if len(r.requests) != 1 || len(r.durations) != 1 {
		t.Fatal("untrusted labels caused cardinality growth")
	}
	r.Begin()
	r.Complete("GET", "/anything/{path:.*}", 200, -time.Second, false, false)
	text := scrape(r).Body.String()
	if strings.Contains(text, "UNTRUSTED") || strings.Contains(text, "user-") || strings.Contains(text, "secret") || strings.ContainsAny(text, "\r\x1b") {
		t.Fatal("unsafe input entered metrics")
	}
	for _, sample := range []string{
		`httptoy_http_requests_total{method="OTHER",route="unmatched",status="other"} 100`,
		`httptoy_http_requests_total{method="GET",route="/anything/{path}",status="200"} 1`,
		`httptoy_http_aborted_requests_total 100`,
		`httptoy_http_panics_total 100`,
		`version="v\"\\\n�"`,
	} {
		if !strings.Contains(text, sample) {
			t.Fatalf("missing escaped/bounded sample %q in %s", sample, text)
		}
	}
}

func TestMetricsConcurrentRequestsAndScrapes(t *testing.T) {
	r := New("", "", []string{"/get"}, nil)
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for j := 0; j < 100; j++ {
				r.Begin()
				r.Complete("GET", "/get", 200, time.Millisecond, false, false)
				w := scrape(r)
				if w.Code != 200 && w.Code != 503 {
					t.Errorf("scrape status=%d", w.Code)
				}
			}
		}()
	}
	group.Wait()
	text := scrape(r).Body.String()
	if !strings.Contains(text, `httptoy_http_requests_total{method="GET",route="/get",status="200"} 800`+"\n") || r.inFlight.Load() != 0 {
		t.Fatal("lost counters or leaked in-flight requests")
	}
}

func TestMetricsScrapeAdmission(t *testing.T) {
	r := New("", "", nil, nil)
	for i := 0; i < cap(r.scrapes); i++ {
		r.scrapes <- struct{}{}
	}
	w := scrape(r)
	if w.Code != 503 || w.Header().Get("Retry-After") != "1" {
		t.Fatal("scrape admission not bounded")
	}
	for i := 0; i < cap(r.scrapes); i++ {
		<-r.scrapes
	}
	if !strings.Contains(scrape(r).Body.String(), `httptoy_admission_rejections_total{pool="metrics"} 1`) {
		t.Fatal("scrape rejection not counted")
	}
}
