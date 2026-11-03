package routes

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/mux"
)

type testSession struct {
	ID      string    `json:"id"`
	Token   string    `json:"token"`
	Expires time.Time `json:"expires_at"`
}

func sessionTestRouter() (*mux.Router, *sessionStore) {
	r, store := NewRouter(), newSessionStore()
	registerSessionRoutes(r, store)
	return r, store
}

func createTestSession(t *testing.T, r http.Handler, config string) testSession {
	t.Helper()
	w := extendedRequest(r, "POST", "/sessions", map[string]string{"Content-Type": "application/json"}, strings.NewReader(config))
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	var session testSession
	if err := json.Unmarshal(w.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if len(session.ID) != 22 || len(session.Token) != 43 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("invalid session credentials/cache policy")
	}
	return session
}

func sessionRequest(r http.Handler, session testSession, method, action, body string, headers map[string]string) *httptest.ResponseRecorder {
	copyHeaders := map[string]string{"Authorization": "Bearer " + session.Token}
	for name, value := range headers {
		copyHeaders[name] = value
	}
	return extendedRequest(r, method, "/sessions/"+session.ID+action, copyHeaders, strings.NewReader(body))
}

func TestSessionAuthorizationAndLifecycle(t *testing.T) {
	r, store := sessionTestRouter()
	now := time.Now()
	store.now = func() time.Time { return now }
	first := createTestSession(t, r, `{"ttl_seconds":2}`)
	second := createTestSession(t, r, "")
	wrong := first
	wrong.Token = second.Token
	for _, session := range []testSession{wrong, {ID: first.ID, Token: ""}, {ID: "unknown", Token: first.Token}} {
		w := sessionRequest(r, session, "GET", "/requests", "", nil)
		if w.Code != 401 || w.Header().Get("WWW-Authenticate") != "Bearer" || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("session isolation failed")
		}
	}
	now = now.Add(2 * time.Second)
	if w := sessionRequest(r, first, "GET", "/requests", "", nil); w.Code != 401 {
		t.Fatal("expired session accepted")
	}
	if _, ok := store.entries[first.ID]; ok {
		t.Fatal("expired session was not reclaimed")
	}
	if w := sessionRequest(r, second, "DELETE", "", "", nil); w.Code != 204 {
		t.Fatal("delete failed")
	}
	if w := sessionRequest(r, second, "GET", "/requests", "", nil); w.Code != 401 {
		t.Fatal("deleted session accepted")
	}
	for _, config := range []string{`null`, `[]`, `{} {}`, `{"unknown":1}`, `{"ttl_seconds":0}`, `{"ttl_seconds":601}`, `{"sequence":[]}`, `{"sequence":[100]}`, `{"rate_limit":0}`, `{"window_seconds":301}`, `{"poll_after":101}`} {
		if w := extendedRequest(r, "POST", "/sessions", nil, strings.NewReader(config)); w.Code != 400 {
			t.Fatalf("bad config %s: %d", config, w.Code)
		}
	}
	if w := extendedRequest(r, "POST", "/sessions", nil, strings.NewReader(strings.Repeat(" ", 8193))); w.Code != 413 {
		t.Fatalf("config body cap=%d", w.Code)
	}
}

func TestSessionCapacity(t *testing.T) {
	r, store := sessionTestRouter()
	for i := 0; i < maxSessions; i++ {
		createTestSession(t, r, "{}")
	}
	w := extendedRequest(r, "POST", "/sessions", nil, nil)
	if w.Code != 503 || len(store.entries) != maxSessions {
		t.Fatal("session capacity unbounded")
	}
}

func TestSessionCapture(t *testing.T) {
	r, _ := sessionTestRouter()
	session := createTestSession(t, r, "{}")
	for _, body := range []string{"one", "two"} {
		w := sessionRequest(r, session, "POST", "/capture?test=yes", body, map[string]string{"X-Test": "value", "Cookie": "real=secret", "X-Api-Key": "sensitive"})
		if w.Code != 202 {
			t.Fatalf("capture: %d %s", w.Code, w.Body)
		}
	}
	w := sessionRequest(r, session, "GET", "/requests", "", nil)
	var result struct {
		Requests []capturedRequest `json:"requests"`
		Count    int               `json:"count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || result.Count != 2 || result.Requests[0].ID != 1 || result.Requests[1].Body != base64.StdEncoding.EncodeToString([]byte("two")) {
		t.Fatal("capture order/body mismatch")
	}
	if strings.Contains(w.Body.String(), session.Token) || strings.Contains(w.Body.String(), "sensitive") || strings.Contains(w.Body.String(), "real=secret") {
		t.Fatal("capture leaked sensitive headers")
	}
	if result.Requests[0].Headers["X-Test"][0] != "value" {
		t.Fatal("safe headers not captured")
	}
	if w := sessionRequest(r, session, "POST", "/capture", strings.Repeat("x", maxCaptureBytes+1), nil); w.Code != 413 {
		t.Fatal("capture body limit ignored")
	}
	for i := 2; i < maxSessionRequests; i++ {
		if w := sessionRequest(r, session, "POST", "/capture", "", nil); w.Code != 202 {
			t.Fatalf("small capture %d: %d", i, w.Code)
		}
	}
	if w := sessionRequest(r, session, "POST", "/capture", "", nil); w.Code != 413 {
		t.Fatal("capture count limit ignored")
	}
	large := createTestSession(t, r, "{}")
	accepted := 0
	for i := 0; i < 10; i++ {
		w := sessionRequest(r, large, "POST", "/capture", strings.Repeat("x", maxCaptureBytes), nil)
		if w.Code == 413 {
			break
		}
		if w.Code != 202 {
			t.Fatal(w.Code)
		}
		accepted++
	}
	if accepted == 0 || accepted >= 10 {
		t.Fatal("capture storage budget not enforced")
	}
}

func TestSessionWorkflow(t *testing.T) {
	r, store := sessionTestRouter()
	now := time.Now()
	store.now = func() time.Time { return now }
	session := createTestSession(t, r, `{"sequence":[503,204,200],"rate_limit":2,"window_seconds":2}`)
	for _, want := range []int{503, 204, 200, 200} {
		w := sessionRequest(r, session, "POST", "/sequence", "", nil)
		if w.Code != want {
			t.Fatalf("sequence=%d want=%d", w.Code, want)
		}
		if want == 204 && w.Body.Len() != 0 {
			t.Fatal("204 must not have a body")
		}
	}
	for _, want := range []int{200, 200, 429} {
		w := sessionRequest(r, session, "GET", "/rate-limit", "", nil)
		if w.Code != want {
			t.Fatalf("rate=%d want=%d", w.Code, want)
		}
		if want == 429 && w.Header().Get("Retry-After") != "2" {
			t.Fatal("missing rate retry window")
		}
	}
	now = now.Add(2 * time.Second)
	if w := sessionRequest(r, session, "GET", "/rate-limit", "", nil); w.Code != 200 || w.Header().Get("X-RateLimit-Remaining") != "1" {
		t.Fatal("rate window failed to reset")
	}
	for _, want := range []int{202, 202, 200, 200} {
		if w := sessionRequest(r, session, "GET", "/poll", "", nil); w.Code != want {
			t.Fatalf("poll=%d want=%d", w.Code, want)
		}
	}
	ready := createTestSession(t, r, `{"poll_after":0}`)
	if w := sessionRequest(r, ready, "GET", "/poll", "", nil); w.Code != 200 {
		t.Fatal("zero pending polls should complete immediately")
	}
}

func TestIdempotencyConcurrency(t *testing.T) {
	r, store := sessionTestRouter()
	session := createTestSession(t, r, "{}")
	const count = 16
	responses := make(chan *httptest.ResponseRecorder, count)
	var group sync.WaitGroup
	for i := 0; i < count; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			responses <- sessionRequest(r, session, "POST", "/idempotency", `{"value":1}`, map[string]string{"Idempotency-Key": "shared", "Content-Type": "application/json"})
		}()
	}
	group.Wait()
	close(responses)
	var first []byte
	created := 0
	for w := range responses {
		if w.Code != 201 {
			t.Fatalf("idempotency=%d %s", w.Code, w.Body)
		}
		if first == nil {
			first = bytes.Clone(w.Body.Bytes())
		}
		if !bytes.Equal(first, w.Body.Bytes()) {
			t.Fatal("concurrent duplicates produced different results")
		}
		if w.Header().Get("Idempotency-Replayed") == "false" {
			created++
		}
	}
	if created != 1 || len(store.entries[session.ID].idempotency) != 1 {
		t.Fatal("duplicate execution")
	}
	if w := sessionRequest(r, session, "POST", "/idempotency", "changed", map[string]string{"Idempotency-Key": "shared"}); w.Code != 409 {
		t.Fatal("conflicting payload accepted")
	}
	for i := 1; i < maxIdempotencyKeys; i++ {
		w := sessionRequest(r, session, "POST", "/idempotency", "", map[string]string{"Idempotency-Key": fmt.Sprintf("key-%d", i)})
		if w.Code != 201 {
			t.Fatal(w.Code)
		}
	}
	if w := sessionRequest(r, session, "POST", "/idempotency", "", map[string]string{"Idempotency-Key": "overflow"}); w.Code != 429 {
		t.Fatal("idempotency map unbounded")
	}
	if w := sessionRequest(r, session, "POST", "/idempotency", strings.Repeat("x", maxBodyBytes+1), map[string]string{"Idempotency-Key": "too-big"}); w.Code != 413 {
		t.Fatal("idempotency body cap ignored")
	}
}

type expireOnRead struct {
	run  func()
	done bool
}

func (r *expireOnRead) Read(p []byte) (int, error) {
	if !r.done {
		r.run()
		r.done = true
		p[0] = 'x'
		return 1, nil
	}
	return 0, io.EOF
}

func TestCaptureRechecksExpiryAfterRead(t *testing.T) {
	r, store := sessionTestRouter()
	now := time.Now()
	store.now = func() time.Time { return now }
	session := createTestSession(t, r, `{"ttl_seconds":1}`)
	req := httptest.NewRequest("POST", "/sessions/"+session.ID+"/capture", &expireOnRead{run: func() { now = now.Add(time.Second) }})
	req.Header.Set("Authorization", "Bearer "+session.Token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatal("expired session modified after body read")
	}
}

type blockingCaptureBody struct {
	started, release chan struct{}
	once             sync.Once
}

type forbiddenSessionBody struct{}

func (forbiddenSessionBody) Read([]byte) (int, error) { panic("unauthorized body was read") }

func TestUnauthorizedSessionDoesNotReadBody(t *testing.T) {
	r, _ := sessionTestRouter()
	for _, action := range []string{"/capture", "/idempotency"} {
		req := httptest.NewRequest("POST", "/sessions/unknown"+action, forbiddenSessionBody{})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 401 {
			t.Fatalf("unauthorized %s: %d", action, w.Code)
		}
	}
}

func (b *blockingCaptureBody) Read([]byte) (int, error) {
	b.once.Do(func() { close(b.started) })
	<-b.release
	return 0, io.EOF
}

func TestSlowCaptureDoesNotHoldStoreLock(t *testing.T) {
	r, _ := sessionTestRouter()
	session := createTestSession(t, r, "{}")
	body := &blockingCaptureBody{started: make(chan struct{}), release: make(chan struct{})}
	req := httptest.NewRequest("POST", "/sessions/"+session.ID+"/capture", body)
	req.Header.Set("Authorization", "Bearer "+session.Token)
	done := make(chan struct{})
	w := httptest.NewRecorder()
	go func() { defer close(done); r.ServeHTTP(w, req) }()
	<-body.started
	rateDone := make(chan *httptest.ResponseRecorder, 1)
	go func() { rateDone <- sessionRequest(r, session, "GET", "/rate-limit", "", nil) }()
	select {
	case response := <-rateDone:
		if response.Code != 200 {
			t.Error("other workflow blocked")
		}
	case <-time.After(time.Second):
		t.Error("slow capture held store lock")
	}
	close(body.release)
	<-done
	if w.Code != 202 {
		t.Fatalf("capture=%d", w.Code)
	}
}
