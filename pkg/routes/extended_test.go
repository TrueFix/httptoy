package routes

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/gorilla/mux"
)

func extendedRequest(r http.Handler, method, path string, headers map[string]string, body io.Reader) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, body)
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestAnythingAndCORS(t *testing.T) {
	r := fixtureRouter()
	for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "CONNECT", "TRACE"} {
		w := extendedRequest(r, method, "/anything/a/b?x=1", map[string]string{"Content-Type": "text/plain", "X-Test": "yes"}, strings.NewReader("hello"))
		var got map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if w.Code != 200 || got["method"] != method || got["path"] != "/anything/a/b" || got["text"] != "hello" {
			t.Fatalf("%s: %s", method, w.Body)
		}
	}
	w := extendedRequest(r, "POST", "/anything", map[string]string{"Content-Type": "application/octet-stream"}, bytes.NewReader([]byte{0, 255, 1}))
	var got struct {
		Body string `json:"body_base64"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Body != base64.StdEncoding.EncodeToString([]byte{0, 255, 1}) {
		t.Fatal("binary echo lost data")
	}
	for _, method := range []string{"POST", "GET"} {
		w := extendedRequest(r, method, "/anything/foo", nil, strings.NewReader(strings.Repeat("x", maxBodyBytes+1)))
		if w.Code != 413 {
			t.Fatalf("size limit: %d", w.Code)
		}
	}
	w = extendedRequest(r, "OPTIONS", "/anything/path?allow_origin=https://client.test&allow_credentials=true&allow_methods=POST&max_age=30", map[string]string{
		"Origin": "https://client.test", "Access-Control-Request-Method": "POST", "Access-Control-Request-Headers": "X-Test, Content-Type",
	}, nil)
	if w.Code != 204 || w.Body.Len() != 0 || w.Header().Get("Access-Control-Allow-Origin") != "https://client.test" || w.Header().Get("Access-Control-Allow-Credentials") != "true" || w.Header().Get("Access-Control-Allow-Headers") != "X-Test, Content-Type" {
		t.Fatalf("preflight: %v", w)
	}
	for _, query := range []string{"allow_credentials=true", "allow_origin=javascript:alert(1)", "allow_methods=GET%0d%0aX-Evil:yes", "allow_headers=X-Test%0aX-Evil", "max_age=999999"} {
		if w := extendedRequest(r, "OPTIONS", "/anything?"+query, nil, nil); w.Code != 400 {
			t.Fatalf("%s: %d", query, w.Code)
		}
	}
}

func TestResponseHeadersAndRedirectTo(t *testing.T) {
	r := fixtureRouter()
	w := extendedRequest(r, "GET", "/response-headers?X-Test=one&X-Test=two&Cache-Control=no-cache", nil, nil)
	if w.Code != 200 || len(w.Header().Values("X-Test")) != 2 || w.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("headers: %v", w)
	}
	for _, query := range []string{"X-Test=a%0d%0aBad:yes", "Bad%20Name=x", "Content-Length=99", "Transfer-Encoding=chunked", "Connection=close", "Content-Encoding=gzip"} {
		if w := extendedRequest(r, "GET", "/response-headers?"+query, nil, nil); w.Code != 400 {
			t.Fatalf("header injection %s: %d", query, w.Code)
		}
	}
	for _, target := range []string{"/get?x=1", "https://example.invalid/destination"} {
		w := extendedRequest(r, "GET", "/redirect-to?status_code=307&url="+url.QueryEscape(target), nil, nil)
		if w.Code != 307 || w.Header().Get("Location") != target {
			t.Fatalf("redirect: %v", w)
		}
	}
	for _, target := range []string{"", "javascript:alert(1)", "file:///etc/passwd", "//example.test", "https://user:password@example.test", "http://example.test/\r\nX-Evil: yes"} {
		w := extendedRequest(r, "GET", "/redirect-to?url="+url.QueryEscape(target), nil, nil)
		if w.Code != 400 {
			t.Fatalf("target %q: %d", target, w.Code)
		}
	}
	if w := extendedRequest(r, "GET", "/redirect-to?url=/get&status_code=304", nil, nil); w.Code != 400 {
		t.Fatal("invalid redirect code accepted")
	}
}

func TestConditionalRequests(t *testing.T) {
	r := fixtureRouter()
	w := extendedRequest(r, "GET", "/cache/60", nil, nil)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "public, max-age=60" {
		t.Fatalf("cache: %v", w)
	}
	for _, headers := range []map[string]string{{"If-None-Match": w.Header().Get("ETag")}, {"If-Modified-Since": w.Header().Get("Last-Modified")}} {
		notModified := extendedRequest(r, "GET", "/cache/60", headers, nil)
		if notModified.Code != 304 || notModified.Body.Len() != 0 {
			t.Fatalf("cache validation: %v", notModified)
		}
	}
	for _, tc := range []struct {
		header, value string
		status        int
	}{
		{"If-None-Match", `"hello"`, 304}, {"If-None-Match", `W/"hello"`, 304}, {"If-None-Match", "*", 304},
		{"If-Match", `"wrong"`, 412}, {"If-Match", `"hello"`, 200},
	} {
		w := extendedRequest(r, "GET", "/etag/hello", map[string]string{tc.header: tc.value}, nil)
		if w.Code != tc.status {
			t.Fatalf("%s %s: %d", tc.header, tc.value, w.Code)
		}
	}
	for _, tc := range []struct {
		rangeValue string
		status     int
		body       string
	}{
		{"bytes=2-5", 206, "cdef"}, {"bytes=-3", 206, "xyz"}, {"bytes=30-40", 416, ""},
	} {
		w := extendedRequest(r, "GET", "/range/26", map[string]string{"Range": tc.rangeValue}, nil)
		if w.Code != tc.status || tc.body != "" && w.Body.String() != tc.body {
			t.Fatalf("range %s: %d %q", tc.rangeValue, w.Code, w.Body.String())
		}
	}
	w = extendedRequest(r, "GET", "/range/26", map[string]string{"Range": "bytes=0-1", "If-Range": `"wrong"`}, nil)
	if w.Code != 200 || w.Body.Len() != 26 {
		t.Fatal("If-Range mismatch must send full body")
	}
	w = extendedRequest(r, "GET", "/range/26", map[string]string{"Range": "bytes=0-1,4-5"}, nil)
	if w.Code != 206 || !strings.HasPrefix(w.Header().Get("Content-Type"), "multipart/byteranges;") {
		t.Fatal("multipart ranges unsupported")
	}
}

func TestStreams(t *testing.T) {
	r := fixtureRouter()
	w := extendedRequest(r, "GET", "/stream/3", nil, nil)
	scanner := bufio.NewScanner(w.Body)
	count := 0
	for scanner.Scan() {
		var record struct {
			ID int `json:"id"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatal(err)
		}
		if record.ID != count {
			t.Fatal("stream order")
		}
		count++
	}
	if w.Code != 200 || count != 3 || !w.Flushed {
		t.Fatalf("NDJSON stream: %v", w)
	}
	w = extendedRequest(r, "GET", "/stream-bytes/10001?chunk_size=127", nil, nil)
	if w.Code != 200 || w.Body.Len() != 10001 || !w.Flushed {
		t.Fatal("binary stream size")
	}
	w = extendedRequest(r, "GET", "/sse?count=3&interval_ms=0", nil, nil)
	if w.Code != 200 || strings.Count(w.Body.String(), "event: message\n") != 3 || !strings.Contains(w.Body.String(), "id: 2\n") {
		t.Fatalf("SSE: %s", w.Body)
	}
	w = extendedRequest(r, "GET", "/drip?numbytes=7&duration=0&delay=0", nil, nil)
	if w.Code != 200 || w.Body.String() != "*******" || w.Header().Get("Content-Length") != "7" || !w.Flushed {
		t.Fatalf("drip: %v", w)
	}
	for _, path := range []string{"/stream/101", "/stream/-1", "/stream-bytes/1048577", "/stream-bytes/5?chunk_size=0", "/sse?count=100&interval_ms=1000", "/drip?numbytes=1025", "/drip?duration=8&delay=8"} {
		if w := extendedRequest(r, "GET", path, nil, nil); w.Code != 400 {
			t.Fatalf("limit %s: %d", path, w.Code)
		}
	}
	for _, path := range []string{"/sse?count=5&interval_ms=1000", "/drip?duration=10"} {
		ctx, cancel := context.WithCancel(context.Background())
		req := httptest.NewRequest("GET", path, nil).WithContext(ctx)
		done := make(chan struct{})
		go func() { defer close(done); r.ServeHTTP(httptest.NewRecorder(), req) }()
		timer := time.AfterFunc(20*time.Millisecond, cancel)
		select {
		case <-done:
		case <-time.After(time.Second):
			cancel()
			t.Fatal("stream ignored cancellation")
		}
		timer.Stop()
		cancel()
	}
	if len(streamSlots) != 0 {
		t.Fatal("leaked stream slots")
	}
}

func TestTrailersOnWire(t *testing.T) {
	srv := httptest.NewServer(fixtureRouter())
	defer srv.Close()
	resp, err := srv.Client().Get(srv.URL + "/trailers")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != DEFAULT_STRING || resp.Trailer.Get("X-Checksum") != checksum(body) || resp.Trailer.Get("X-Stream-Complete") != "true" {
		t.Fatalf("trailers: %v", resp.Trailer)
	}
}

func TestUpload(t *testing.T) {
	r := fixtureRouter()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("tag", "one"); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("tag", "two"); err != nil {
		t.Fatal(err)
	}
	file, err := writer.CreateFormFile("attachment", "test.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(file, "hello"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	w := extendedRequest(r, "POST", "/upload", map[string]string{"Content-Type": writer.FormDataContentType()}, bytes.NewReader(body.Bytes()))
	var got struct {
		Fields map[string][]string `json:"fields"`
		Files  []uploadedFile      `json:"files"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || len(got.Fields["tag"]) != 2 || len(got.Files) != 1 || got.Files[0].Name != "test.txt" || got.Files[0].Size != 5 || got.Files[0].SHA256 != checksum([]byte("hello")) || !strings.HasPrefix(got.Files[0].ContentType, "text/plain") {
		t.Fatalf("upload: %d %s", w.Code, w.Body)
	}
	oversized := append(bytes.Clone(body.Bytes()), bytes.Repeat([]byte("x"), maxBodyBytes)...)
	w = extendedRequest(r, "POST", "/upload", map[string]string{"Content-Type": writer.FormDataContentType()}, bytes.NewReader(oversized))
	if w.Code != 413 {
		t.Fatalf("upload epilogue limit: %d", w.Code)
	}
	w = extendedRequest(r, "POST", "/upload", nil, strings.NewReader("broken"))
	if w.Code != 400 {
		t.Fatal("non-multipart upload accepted")
	}
}

func TestUtilitiesAndLimits(t *testing.T) {
	r := fixtureRouter()
	for _, path := range []string{"/healthz", "/robots.txt", "/links/3"} {
		w := extendedRequest(r, "GET", path, nil, nil)
		if w.Code != 200 || w.Body.Len() == 0 {
			t.Fatalf("%s: %d", path, w.Code)
		}
		if path == "/links/3" && strings.Count(w.Body.String(), "<a href=") != 3 {
			t.Fatal("wrong link count")
		}
	}
	for _, path := range []string{"/links/101", "/cache/-1", "/cache/86401", "/range/1048577", "/range/invalid", "/etag/a%22b"} {
		if w := extendedRequest(r, "GET", path, nil, nil); w.Code != 400 {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}
}

func TestWebSocketEcho(t *testing.T) {
	srv := httptest.NewServer(fixtureRouter())
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	for _, kind := range []websocket.MessageType{websocket.MessageText, websocket.MessageBinary} {
		want := []byte("hello")
		if kind == websocket.MessageBinary {
			want = []byte{0, 255, 1}
		}
		if err := conn.Write(ctx, kind, want); err != nil {
			t.Fatal(err)
		}
		gotKind, got, err := conn.Read(ctx)
		if err != nil || gotKind != kind || !bytes.Equal(got, want) {
			t.Fatalf("echo kind=%v body=%v error=%v", gotKind, got, err)
		}
	}
	conn.Close(websocket.StatusNormalClosure, "")
	_, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{"https://evil.test"}}})
	if err == nil || resp == nil || resp.StatusCode != 403 {
		t.Fatal("cross-origin WebSocket accepted")
	}
	if w := extendedRequest(fixtureRouter(), "GET", "/ws", nil, nil); w.Code != 426 {
		t.Fatalf("nonupgrade=%d", w.Code)
	}
}

func TestDigestAuthentication(t *testing.T) {
	r := fixtureRouter()
	w := extendedRequest(r, "GET", "/auth/digest", nil, nil)
	challenge, ok := parseDigest(w.Header().Get("WWW-Authenticate"))
	if w.Code != 401 || !ok || challenge["algorithm"] != "SHA-256" || challenge["qop"] != "auth" {
		t.Fatalf("challenge %v", w)
	}
	makeAuth := func(nonce, uri, password, nc string) string {
		ha1 := checksum([]byte("user:httptoy:" + password))
		ha2 := checksum([]byte("GET:" + uri))
		response := checksum([]byte(ha1 + ":" + nonce + ":" + nc + ":test:auth:" + ha2))
		return fmt.Sprintf(`Digest username="user", realm="httptoy", nonce="%s", uri="%s", algorithm=SHA-256, qop=auth, nc=%s, cnonce="test", response="%s"`, nonce, uri, nc, response)
	}
	nonce := challenge["nonce"]
	for _, auth := range []string{makeAuth(nonce, "/auth/digest", "wrong", "00000001"), makeAuth(nonce, "/other", "passwd", "00000001"), makeAuth(signedNonce(time.Now().Add(-10*time.Minute)), "/auth/digest", "passwd", "00000001"), makeAuth("invalid", "/auth/digest", "passwd", "00000001")} {
		if w := extendedRequest(r, "GET", "/auth/digest", map[string]string{"Authorization": auth}, nil); w.Code != 401 {
			t.Fatal("invalid digest accepted")
		}
	}
	auth := makeAuth(nonce, "/auth/digest", "passwd", "00000001")
	for i, want := range []int{200, 401} {
		w := extendedRequest(r, "GET", "/auth/digest", map[string]string{"Authorization": auth}, nil)
		if w.Code != want {
			t.Fatalf("attempt %d status=%d", i, w.Code)
		}
	}
	w = extendedRequest(r, "GET", "/auth/digest", map[string]string{"Authorization": makeAuth(nonce, "/auth/digest", "passwd", "00000002")}, nil)
	if w.Code != 200 {
		t.Fatal("increasing nonce count rejected")
	}
	for _, header := range []string{`Digest username="a", username="b"`, `Digest username="unterminated`, `Digest username="a" extra`, `Basic user:passwd`} {
		if _, ok := parseDigest(header); ok {
			t.Fatalf("malformed header %q accepted", header)
		}
	}
	if _, ok := nonceExpiry(signedNonce(time.Now().Add(time.Hour)), time.Now()); ok {
		t.Fatal("future nonce accepted")
	}
}

func TestSSEFlushOnWire(t *testing.T) {
	srv := httptest.NewServer(fixtureRouter())
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", srv.URL+"/sse?count=2&interval_ms=1000", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	if err != nil || line != "id: 0\n" {
		t.Fatalf("first SSE event was buffered: %q %v", line, err)
	}
	cancel()
}

func TestWebSocketLimits(t *testing.T) {
	srv := httptest.NewServer(fixtureRouter())
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	if err := conn.Write(ctx, websocket.MessageBinary, make([]byte, (64<<10)+1)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := conn.Read(ctx); websocket.CloseStatus(err) != websocket.StatusMessageTooBig {
		t.Fatalf("oversized message: %v", err)
	}
	for i := 0; i < cap(websocketSlots); i++ {
		websocketSlots <- struct{}{}
	}
	defer func() {
		for len(websocketSlots) > 0 {
			<-websocketSlots
		}
	}()
	if w := extendedRequest(fixtureRouter(), "GET", "/ws", nil, nil); w.Code != 503 {
		t.Fatal("WebSocket admission limit ignored")
	}
}

func TestStreamAdmission(t *testing.T) {
	for i := 0; i < cap(streamSlots); i++ {
		streamSlots <- struct{}{}
	}
	defer func() {
		for len(streamSlots) > 0 {
			<-streamSlots
		}
	}()
	w := extendedRequest(fixtureRouter(), "GET", "/stream/1", nil, nil)
	if w.Code != 503 || w.Header().Get("Retry-After") == "" {
		t.Fatal("stream budget not enforced")
	}
}

func TestExtendedRouteContract(t *testing.T) {
	r := fixtureRouter()
	routes := registeredRoutes(t, r)
	for _, path := range []string{"/anything", "/anything/{path:.*}"} {
		for _, method := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "CONNECT", "TRACE", "OPTIONS"} {
			if !routes[method+" "+path] {
				t.Fatalf("missing %s %s", method, path)
			}
		}
	}
	for i, path := range []string{"/stream/{count}", "/stream-bytes/{count}", "/response-headers", "/redirect-to", "/cache/{seconds}", "/etag/{value}", "/range/{size}", "/healthz", "/sse", "/ws", "/drip", "/trailers", "/links/{count}", "/robots.txt", "/auth/digest"} {
		if !routes["GET "+path] {
			t.Fatal("missing route " + strconv.Itoa(i) + " " + path)
		}
	}
	if !routes["POST /upload"] {
		t.Fatal("missing upload")
	}
}

// Ensure these tests exercise the production route registry rather than test-
// only handlers. This compile-time check also documents the expected router type.
var _ http.Handler = (*mux.Router)(nil)
