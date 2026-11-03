// Package metrics implements bounded, process-local Prometheus text metrics.
package metrics

import (
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

var processStarted = time.Now()
var variable = regexp.MustCompile(`\{([A-Za-z0-9_]+)(?::[^}]+)?\}`)
var buckets = [...]float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}

// RouteLabel normalizes a trusted router template, never a raw request path.
func RouteLabel(template string) string { return variable.ReplaceAllString(template, "{$1}") }

type Pool struct {
	Name             string
	Active, Capacity int
	Rejected         uint64
}
type Resources struct {
	Pools                                                                       []Pool
	SessionsActive, CaptureBytes, CapturedRequests, IdempotencyKeys, SSEClients int
}

type requestKey struct{ method, route, status string }
type durationKey struct{ method, route string }
type histogram struct {
	buckets [len(buckets)]uint64
	count   uint64
	sum     float64
}

// Registry has its own request counters, while resources describe process-wide
// pools. Allowed route labels are fixed at construction to bound cardinality.
type Registry struct {
	mu                              sync.Mutex
	allowed                         map[string]bool
	requests                        map[requestKey]uint64
	durations                       map[durationKey]histogram
	version, sha                    string
	resources                       func() Resources
	inFlight                        atomic.Int64
	aborted, panics, scrapeRejected atomic.Uint64
	scrapes                         chan struct{}
}

func New(version, sha string, templates []string, resources func() Resources) *Registry {
	r := &Registry{allowed: map[string]bool{"unmatched": true}, requests: make(map[requestKey]uint64), durations: make(map[durationKey]histogram), version: buildLabel(version, "unknown"), sha: buildLabel(sha, "development"), resources: resources, scrapes: make(chan struct{}, 2)}
	for _, template := range templates {
		r.allowed[RouteLabel(template)] = true
	}
	return r
}

func buildLabel(value, fallback string) string {
	if value == "" {
		return fallback
	}
	if len(value) > 128 {
		value = value[:128]
	}
	value = strings.ToValidUTF8(value, "�")
	return strings.Map(func(ch rune) rune {
		if ch < 32 && ch != '\n' || ch == 127 {
			return '�'
		}
		return ch
	}, value)
}

func (r *Registry) Begin() { r.inFlight.Add(1) }

// Complete must pair with Begin, including panic, cancellation and upgrade exits.
func (r *Registry) Complete(method, template string, status int, duration time.Duration, aborted, panicked bool) {
	defer r.inFlight.Add(-1)
	method = methodLabel(method)
	route := RouteLabel(template)
	if !r.allowed[route] {
		route = "unmatched"
	}
	code := "other"
	if http.StatusText(status) != "" {
		code = strconv.Itoa(status)
	}
	seconds := max(0, duration.Seconds())
	r.mu.Lock()
	r.requests[requestKey{method, route, code}]++
	key := durationKey{method, route}
	hist := r.durations[key]
	hist.count++
	hist.sum += seconds
	for i, bound := range buckets {
		if seconds <= bound {
			hist.buckets[i]++
		}
	}
	r.durations[key] = hist
	r.mu.Unlock()
	if aborted {
		r.aborted.Add(1)
	}
	if panicked {
		r.panics.Add(1)
	}
}

func methodLabel(method string) string {
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "CONNECT", "TRACE":
		return method
	default:
		return "OTHER"
	}
}

func quoted(value string) string {
	if !utf8.ValidString(value) {
		value = strings.ToValidUTF8(value, "�")
	}
	return `"` + strings.NewReplacer(`\`, `\\`, "\n", `\n`, `"`, `\"`).Replace(value) + `"`
}

func family(b *strings.Builder, name, help, kind string) {
	fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, kind)
}

func (r *Registry) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	select {
	case r.scrapes <- struct{}{}:
		defer func() { <-r.scrapes }()
	default:
		r.scrapeRejected.Add(1)
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	// Snapshot under the lock; formatting, resource callbacks and network writes
	// must not block request-counter updates or hold a session-store lock.
	r.mu.Lock()
	requests := make(map[requestKey]uint64, len(r.requests))
	for key, value := range r.requests {
		requests[key] = value
	}
	durations := make(map[durationKey]histogram, len(r.durations))
	for key, value := range r.durations {
		durations[key] = value
	}
	r.mu.Unlock()
	resources := Resources{}
	if r.resources != nil {
		resources = r.resources()
	}
	resources.Pools = append(append([]Pool{}, resources.Pools...), Pool{Name: "metrics", Active: len(r.scrapes), Capacity: cap(r.scrapes), Rejected: r.scrapeRejected.Load()})
	var b strings.Builder
	family(&b, "httptoy_info", "httptoy build information.", "gauge")
	fmt.Fprintf(&b, "httptoy_info{version=%s,git_sha=%s} 1\n", quoted(r.version), quoted(r.sha))
	family(&b, "httptoy_uptime_seconds", "Process uptime in seconds.", "gauge")
	fmt.Fprintf(&b, "httptoy_uptime_seconds %g\n", time.Since(processStarted).Seconds())
	family(&b, "httptoy_http_requests_in_flight", "HTTP handlers currently executing, including streams and WebSockets.", "gauge")
	fmt.Fprintf(&b, "httptoy_http_requests_in_flight %d\n", r.inFlight.Load())
	family(&b, "httptoy_http_requests_total", "Completed HTTP handlers by method, route template and recorded response status.", "counter")
	keys := make([]requestKey, 0, len(requests))
	for key := range requests {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, c := keys[i], keys[j]
		if a.route != c.route {
			return a.route < c.route
		}
		if a.method != c.method {
			return a.method < c.method
		}
		return a.status < c.status
	})
	for _, key := range keys {
		fmt.Fprintf(&b, "httptoy_http_requests_total{method=%s,route=%s,status=%s} %d\n", quoted(key.method), quoted(key.route), quoted(key.status), requests[key])
	}
	family(&b, "httptoy_http_request_duration_seconds", "Full handler duration, including intentional delays and streaming lifetimes.", "histogram")
	durationKeys := make([]durationKey, 0, len(durations))
	for key := range durations {
		durationKeys = append(durationKeys, key)
	}
	sort.Slice(durationKeys, func(i, j int) bool {
		if durationKeys[i].route != durationKeys[j].route {
			return durationKeys[i].route < durationKeys[j].route
		}
		return durationKeys[i].method < durationKeys[j].method
	})
	for _, key := range durationKeys {
		hist := durations[key]
		labels := "method=" + quoted(key.method) + ",route=" + quoted(key.route)
		for i, bound := range buckets {
			fmt.Fprintf(&b, "httptoy_http_request_duration_seconds_bucket{%s,le=%s} %d\n", labels, quoted(strconv.FormatFloat(bound, 'g', -1, 64)), hist.buckets[i])
		}
		fmt.Fprintf(&b, "httptoy_http_request_duration_seconds_bucket{%s,le=\"+Inf\"} %d\nhttptoy_http_request_duration_seconds_sum{%s} %g\nhttptoy_http_request_duration_seconds_count{%s} %d\n", labels, hist.count, labels, hist.sum, labels, hist.count)
	}
	for _, metric := range []struct {
		name, help string
		value      uint64
	}{{"httptoy_http_aborted_requests_total", "Handlers aborted or ending with a canceled request context.", r.aborted.Load()}, {"httptoy_http_panics_total", "Recovered unexpected handler panics (excluding intentional aborts).", r.panics.Load()}} {
		family(&b, metric.name, metric.help, "counter")
		fmt.Fprintf(&b, "%s %d\n", metric.name, metric.value)
	}
	family(&b, "httptoy_resource_pool_active", "Currently occupied resource slots.", "gauge")
	for _, pool := range resources.Pools {
		fmt.Fprintf(&b, "httptoy_resource_pool_active{pool=%s} %d\n", quoted(pool.Name), pool.Active)
	}
	family(&b, "httptoy_resource_pool_capacity", "Configured resource slot capacity.", "gauge")
	for _, pool := range resources.Pools {
		fmt.Fprintf(&b, "httptoy_resource_pool_capacity{pool=%s} %d\n", quoted(pool.Name), pool.Capacity)
	}
	family(&b, "httptoy_admission_rejections_total", "Requests rejected because a resource pool was full.", "counter")
	for _, pool := range resources.Pools {
		fmt.Fprintf(&b, "httptoy_admission_rejections_total{pool=%s} %d\n", quoted(pool.Name), pool.Rejected)
	}
	for _, metric := range []struct {
		name, help string
		value      int
	}{
		{"httptoy_sessions_active", "Active unexpired workflow sessions.", resources.SessionsActive},
		{"httptoy_session_capture_bytes", "Accounted retained capture bytes (not total process memory).", resources.CaptureBytes},
		{"httptoy_session_captured_requests", "Currently retained captured requests.", resources.CapturedRequests},
		{"httptoy_session_idempotency_keys", "Currently retained idempotency keys.", resources.IdempotencyKeys},
		{"httptoy_sse_clients", "Currently executing SSE handlers, including resumable streams.", resources.SSEClients},
	} {
		family(&b, metric.name, metric.help, "gauge")
		fmt.Fprintf(&b, "%s %d\n", metric.name, metric.value)
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = fmt.Fprint(w, b.String())
}
