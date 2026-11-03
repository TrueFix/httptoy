package routes

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/gorilla/mux"
)

func protocolTestRouter() *mux.Router { r := mux.NewRouter(); addProtocolRoutes(r); return r }

func protocolTestRequest(method, path string, body io.Reader, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, body)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	protocolTestRouter().ServeHTTP(w, req)
	return w
}

func TestProtocolsNegotiation(t *testing.T) {
	for _, tc := range []struct {
		accept, want string
		status       int
	}{
		{"", "application/json", 200},
		{"application/json;q=0.2,text/plain;q=0.9", "text/plain", 200},
		{"text/*;q=0.8,application/json;q=0.7", "text/html", 200},
		{"*/*;q=1,application/json;q=0", "application/xml", 200},
		{"*/*;q=0,text/plain;q=0.1", "text/plain", 200},
		{"application/*;q=0,*/*;q=0.5", "text/html", 200},
		{"text/html;q=0,text/*;q=0.7", "text/plain", 200},
		{"image/png", "", 406},
		{"*/*;q=0", "", 406},
		{"application/json;charset=iso-8859-1", "", 406},
		{"application/json;charset=utf-8", "application/json", 200},
		{"application/json;q=1.1", "", 400},
		{"application/json;q=0.1234", "", 400},
		{"*/json", "", 400},
		{strings.Repeat("x", 4097), "", 400},
		{strings.Repeat("text/plain,", 32) + "text/plain", "", 400},
	} {
		t.Run(tc.accept, func(t *testing.T) {
			w := protocolTestRequest("GET", "/negotiate", nil, map[string]string{"Accept": tc.accept})
			if w.Code != tc.status || tc.want != "" && !strings.HasPrefix(w.Header().Get("Content-Type"), tc.want) {
				t.Fatalf("got %d %s %s", w.Code, w.Header().Get("Content-Type"), w.Body.String())
			}
			if w.Header().Get("Vary") != "Accept" {
				t.Fatal("missing Vary")
			}
		})
	}
	for _, tc := range []struct {
		header, language string
		status           int
	}{
		{"", "en", 200}, {"fr;q=0.8,es;q=0.9,en;q=0.1", "es", 200},
		{"fr-CA,es;q=0.5", "fr", 200}, {"*;q=1,en;q=0", "es", 200},
		{"es-419", "es", 200},
		{"en-US;q=0,en;q=1", "en", 200},
		{"en-US;q=0,*;q=1", "en", 200},
		{"*;q=0,fr;q=0.2", "fr", 200}, {"de", "", 406}, {"*;q=0", "", 406}, {"en;q=no", "", 400},
	} {
		w := protocolTestRequest("GET", "/language", nil, map[string]string{"Accept-Language": tc.header})
		if w.Code != tc.status || w.Header().Get("Content-Language") != tc.language || w.Header().Get("Vary") != "Accept-Language" {
			t.Fatalf("%q: got %d %v", tc.header, w.Code, w.Header())
		}
	}
}

func TestProtocolsProblem(t *testing.T) {
	for _, status := range []int{400, 404, 418, 429, 500, 503} {
		w := protocolTestRequest("GET", fmt.Sprintf("/problem/%d", status), nil, nil)
		var p struct {
			Type, Title string
			Status      int
		}
		if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		if w.Code != status || p.Status != status || p.Title != http.StatusText(status) || p.Type != "about:blank" || w.Header().Get("Content-Type") != "application/problem+json" {
			t.Fatalf("bad problem: %d %s", w.Code, w.Body.String())
		}
	}
	for _, status := range []string{"200", "399", "600", "499", "abc"} {
		if w := protocolTestRequest("GET", "/problem/"+status, nil, nil); w.Code != 400 {
			t.Fatalf("%s: %d", status, w.Code)
		}
	}
}

func TestProtocolsResume(t *testing.T) {
	w := protocolTestRequest("GET", "/sse/resume?count=3&interval_ms=0", nil, map[string]string{"Last-Event-ID": "41"})
	if w.Code != 200 || !strings.Contains(w.Body.String(), "id: 42\n") || !strings.Contains(w.Body.String(), "id: 44\n") || strings.Count(w.Body.String(), "event: message") != 3 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = protocolTestRequest("GET", "/sse/resume?count=1&interval_ms=0", nil, nil)
	if !strings.Contains(w.Body.String(), "id: 0\n") {
		t.Fatal(w.Body.String())
	}
	for _, path := range []string{"?count=101", "?count=-1", "?interval_ms=1001", "?count=100&interval_ms=102", "?interval_ms=x"} {
		if w := protocolTestRequest("GET", "/sse/resume"+path, nil, nil); w.Code != 400 {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}
	for _, id := range []string{"-1", "x", "9223372036854775807", strings.Repeat("1", 100)} {
		if w := protocolTestRequest("GET", "/sse/resume?interval_ms=0", nil, map[string]string{"Last-Event-ID": id}); w.Code != 400 {
			t.Fatal(id, w.Code)
		}
	}
	req := httptest.NewRequest("GET", "/sse/resume?count=100&interval_ms=100", nil)
	ctx, cancel := context.WithCancel(req.Context())
	cancel()
	w = httptest.NewRecorder()
	protocolTestRouter().ServeHTTP(w, req.WithContext(ctx))
	if w.Body.Len() != 0 {
		t.Fatal("wrote after cancellation")
	}
	if len(streamSlots) != 0 {
		t.Fatal("leaked stream slot")
	}
}

type protocolObservedBody struct{ reads int }

func (b *protocolObservedBody) Read(p []byte) (int, error) { b.reads++; return 0, io.EOF }
func (b *protocolObservedBody) Close() error               { return nil }

func TestProtocolsExpectNoRead(t *testing.T) {
	body := &protocolObservedBody{}
	req := httptest.NewRequest("POST", "/expect-continue?reject=true", nil)
	req.Body = body
	req.Header.Set("Expect", "100-continue")
	w := httptest.NewRecorder()
	protocolTestRouter().ServeHTTP(w, req)
	if w.Code != 417 || body.reads != 0 {
		t.Fatalf("status=%d body reads=%d", w.Code, body.reads)
	}
	w = protocolTestRequest("POST", "/expect-continue", strings.NewReader("hello"), nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"bytes":5`) {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestProtocolsExpectInvalidBooleanAndHTTP2(t *testing.T) {
	for _, tc := range []struct {
		query         string
		major, status int
	}{{"bad", 1, 400}, {"true", 2, 417}} {
		body := &protocolObservedBody{}
		req := httptest.NewRequest("POST", "/expect-continue?reject="+tc.query, nil)
		req.Body = body
		req.ProtoMajor = tc.major
		w := httptest.NewRecorder()
		protocolTestRouter().ServeHTTP(w, req)
		if w.Code != tc.status || body.reads != 0 {
			t.Fatalf("expect rejection: %d reads=%d", w.Code, body.reads)
		}
		if tc.major == 2 && w.Header().Get("Connection") != "" {
			t.Fatal("HTTP/2 response included a connection-specific header")
		}
	}
}

func TestProtocolsExpectOnWire(t *testing.T) {
	server := httptest.NewServer(protocolTestRouter())
	defer server.Close()
	conn, err := net.DialTimeout("tcp", server.Listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	_, err = fmt.Fprint(conn, "POST /expect-continue?reject=true HTTP/1.1\r\nHost: example\r\nContent-Length: 100\r\nExpect: 100-continue\r\n\r\n")
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 417 {
		t.Fatalf("expected final rejection without 100 Continue; got %d", response.StatusCode)
	}
}

func TestProtocolsExpectAcceptedOnWire(t *testing.T) {
	server := httptest.NewServer(protocolTestRouter())
	defer server.Close()
	conn, err := net.DialTimeout("tcp", server.Listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	_, err = fmt.Fprint(conn, "POST /expect-continue HTTP/1.1\r\nHost: example\r\nContent-Length: 5\r\nExpect: 100-continue\r\n\r\n")
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	interim, err := http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = interim.Body.Close()
	if interim.StatusCode != 100 {
		t.Fatalf("expected Continue, got %d", interim.StatusCode)
	}
	if _, err := io.WriteString(conn, "hello"); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || !bytes.Contains(data, []byte(`"bytes":5`)) {
		t.Fatal(response.StatusCode, string(data))
	}
}

func TestProtocolsResumeStreamBudget(t *testing.T) {
	for i := 0; i < cap(streamSlots); i++ {
		streamSlots <- struct{}{}
	}
	defer func() {
		for i := 0; i < cap(streamSlots); i++ {
			<-streamSlots
		}
	}()
	w := protocolTestRequest("GET", "/sse/resume?interval_ms=0", nil, nil)
	if w.Code != 503 || w.Header().Get("Retry-After") != "1" {
		t.Fatal(w.Code, w.Header())
	}
}

func TestProtocolsTrailerLimit(t *testing.T) {
	req := httptest.NewRequest("POST", "/request-trailers", strings.NewReader("body"))
	req.Trailer = http.Header{"X-Large": []string{strings.Repeat("x", 4097)}}
	w := httptest.NewRecorder()
	protocolTestRouter().ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	req = httptest.NewRequest("POST", "/request-trailers", strings.NewReader("body"))
	values := make([]string, 32)
	for i := range values {
		values[i] = "v"
	}
	req.Trailer = http.Header{"X-One": values, "X-Two": append([]string{}, values...)}
	w = httptest.NewRecorder()
	protocolTestRouter().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatal("32-values-per-field contract rejected bounded trailer fields")
	}
}

func TestProtocolWorkAdmission(t *testing.T) {
	for i := 0; i < cap(protocolWorkSlots); i++ {
		protocolWorkSlots <- struct{}{}
	}
	defer func() {
		for len(protocolWorkSlots) > 0 {
			<-protocolWorkSlots
		}
	}()
	for _, path := range []string{"/decompress/gzip", "/validate/json"} {
		w := protocolTestRequest("POST", path, strings.NewReader("{}"), nil)
		if w.Code != 503 || w.Header().Get("Retry-After") != "1" {
			t.Fatalf("work budget not enforced for %s", path)
		}
	}
}

func TestProtocolsRequestTrailersOnWire(t *testing.T) {
	server := httptest.NewServer(protocolTestRouter())
	defer server.Close()
	req, err := http.NewRequest("POST", server.URL+"/request-trailers", io.NopCloser(strings.NewReader("hello")))
	if err != nil {
		t.Fatal(err)
	}
	req.Trailer = http.Header{"X-Checksum": []string{"done"}}
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || !bytes.Contains(data, []byte(`"X-Checksum":["done"]`)) || !bytes.Contains(data, []byte(`"bytes":5`)) {
		t.Fatal(response.StatusCode, string(data))
	}
}

func protocolCompressed(t testing.TB, coding string, data []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	var writer io.WriteCloser
	switch coding {
	case "gzip":
		writer = gzip.NewWriter(&buffer)
	case "deflate":
		writer = zlib.NewWriter(&buffer)
	case "br":
		writer = brotli.NewWriter(&buffer)
	}
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestProtocolsDecompress(t *testing.T) {
	for _, coding := range []string{"gzip", "deflate", "br"} {
		t.Run(coding, func(t *testing.T) {
			for _, n := range []int{0, 100, maxBodyBytes, maxBodyBytes + 1} {
				payload := bytes.Repeat([]byte("x"), n)
				compressed := protocolCompressed(t, coding, payload)
				// Unknown length models chunked requests and exercises the read cap.
				req := httptest.NewRequest("POST", "/decompress/"+coding, io.NopCloser(bytes.NewReader(compressed)))
				w := httptest.NewRecorder()
				protocolTestRouter().ServeHTTP(w, req)
				want := 200
				if n > maxBodyBytes {
					want = 413
				}
				if w.Code != want || want == 200 && !bytes.Equal(w.Body.Bytes(), payload) {
					t.Fatalf("n=%d: got %d (%d bytes)", n, w.Code, w.Body.Len())
				}
			}
			for _, data := range [][]byte{[]byte("not compressed"), protocolCompressed(t, coding, []byte("payload"))[:3]} {
				w := protocolTestRequest("POST", "/decompress/"+coding, bytes.NewReader(data), nil)
				if w.Code != 400 {
					t.Fatalf("invalid stream: %d", w.Code)
				}
			}
			// Even ignored trailing wire bytes must count toward compressed cap.
			data := append(protocolCompressed(t, coding, nil), make([]byte, maxBodyBytes+1)...)
			w := protocolTestRequest("POST", "/decompress/"+coding, io.NopCloser(bytes.NewReader(data)), nil)
			if w.Code != 413 {
				t.Fatal("wire cap", w.Code)
			}
		})
	}
	member := protocolCompressed(t, "gzip", bytes.Repeat([]byte("x"), maxBodyBytes/2+1))
	w := protocolTestRequest("POST", "/decompress/gzip", bytes.NewReader(append(append([]byte{}, member...), member...)), nil)
	if w.Code != 413 {
		t.Fatal("multimember bomb", w.Code)
	}
	member = protocolCompressed(t, "gzip", []byte("hello"))
	w = protocolTestRequest("POST", "/decompress/gzip", bytes.NewReader(append(append([]byte{}, member...), member...)), nil)
	if w.Code != 200 || w.Body.String() != "hellohello" {
		t.Fatal("multimember decode", w.Code, w.Body.String())
	}
	member[len(member)-1] ^= 0xff
	w = protocolTestRequest("POST", "/decompress/gzip", bytes.NewReader(member), nil)
	if w.Code != 400 {
		t.Fatal("checksum", w.Code)
	}
	if w := protocolTestRequest("POST", "/decompress/unknown", nil, nil); w.Code != 400 {
		t.Fatal(w.Code)
	}
}

func TestProtocolsValidateJSONXML(t *testing.T) {
	for _, tc := range []struct {
		format, data string
		status       int
	}{
		{"json", `null`, 200}, {"json", `true`, 200}, {"json", `123`, 200}, {"json", `"scalar"`, 200}, {"json", `[]`, 200}, {"json", `{}`, 200},
		{"json", `{"a":1} `, 200}, {"json", `{} {}`, 400}, {"json", `{}x`, 400}, {"json", ``, 400}, {"json", `{"a":`, 400},
		{"xml", `<a/>`, 200}, {"xml", `<?xml version="1.0"?><a><b>text</b></a>`, 200},
		{"xml", `<!--comment--><a/><!--end-->`, 200}, {"xml", ``, 400}, {"xml", `<a/><b/>`, 400},
		{"xml", `text<a/>`, 400}, {"xml", `<a/>tail`, 400}, {"xml", `<a>`, 400}, {"xml", `<a></b>`, 400},
		{"xml", `<a x="1" x="2"/>`, 400}, {"xml", `<!DOCTYPE a><a/>`, 400},
		{"xml", `<a/><!----><?xml version="1.0"?>`, 400},
		{"xml", ` <?xml version="1.0"?><a/>`, 400},
		{"xml", `<?xml version="1.0"?><?xml version="1.0"?><a/>`, 400},
		{"xml", `<?XML version="1.0"?><a/>`, 400},
		{"xml", `<?xml?><a/>`, 400},
		{"xml", "\xef\xbb\xbf<?xml version='1.0' encoding='UTF-8' standalone='yes'?><a/>", 200},
		{"xml", strings.Repeat("<a>", 129) + strings.Repeat("</a>", 129), 400},
		{"xml", strings.Repeat("<a>", 128) + strings.Repeat("</a>", 128), 200},
	} {
		w := protocolTestRequest("POST", "/validate/"+tc.format, strings.NewReader(tc.data), nil)
		if w.Code != tc.status {
			t.Fatalf("%s %.60q: %d %s", tc.format, tc.data, w.Code, w.Body.String())
		}
		if w.Code == 400 && w.Body.Len() > 512 {
			t.Fatal("unbounded parse error")
		}
	}
	if w := protocolTestRequest("POST", "/validate/yaml", nil, nil); w.Code != 400 {
		t.Fatal(w.Code)
	}
}

func TestProtocolsMultipart(t *testing.T) {
	for _, n := range []int{1, 100, 101} {
		var data bytes.Buffer
		writer := multipart.NewWriter(&data)
		for i := 0; i < n; i++ {
			part, err := writer.CreateFormFile("file", "test.txt")
			if err != nil {
				t.Fatal(err)
			}
			_, _ = io.WriteString(part, "contents")
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		w := protocolTestRequest("POST", "/validate/multipart", bytes.NewReader(data.Bytes()), map[string]string{"Content-Type": writer.FormDataContentType()})
		want := 200
		if n > 100 {
			want = 400
		}
		if w.Code != want {
			t.Fatal(n, w.Code, w.Body.String())
		}
		truncated := protocolTestRequest("POST", "/validate/multipart", bytes.NewReader(data.Bytes()[:data.Len()-10]), map[string]string{"Content-Type": writer.FormDataContentType()})
		if truncated.Code != 400 {
			t.Fatal("accepted truncated multipart", truncated.Code)
		}
	}
	for _, ct := range []string{"", "application/json", "multipart/form-data", `multipart/form-data; boundary="` + strings.Repeat("a", 71) + `"`} {
		if w := protocolTestRequest("POST", "/validate/multipart", strings.NewReader("x"), map[string]string{"Content-Type": ct}); w.Code != 400 {
			t.Fatal(ct, w.Code)
		}
	}
}

func TestProtocolsBodyLimits(t *testing.T) {
	data := strings.Repeat("x", maxBodyBytes+1)
	for _, path := range []string{"/expect-continue", "/request-trailers", "/validate/json", "/validate/xml", "/validate/multipart", "/decompress/gzip"} {
		for _, chunked := range []bool{false, true} {
			var body io.Reader = strings.NewReader(data)
			if chunked {
				body = io.NopCloser(body)
			}
			w := protocolTestRequest("POST", path, body, nil)
			if w.Code != 413 {
				t.Fatalf("%s chunked=%v: %d", path, chunked, w.Code)
			}
		}
	}
	// A known oversized payload should be rejected without reading it.
	body := &protocolObservedBody{}
	req := httptest.NewRequest("POST", "/expect-continue", nil)
	req.Body, req.ContentLength = body, maxBodyBytes+1
	w := httptest.NewRecorder()
	protocolTestRouter().ServeHTTP(w, req)
	if w.Code != 413 || body.reads != 0 {
		t.Fatal(w.Code, body.reads)
	}
}

func BenchmarkProtocolsNegotiate(b *testing.B) {
	router := protocolTestRouter()
	req := httptest.NewRequest("GET", "/negotiate", nil)
	req.Header.Set("Accept", "application/json;q=0.2,text/plain;q=0.9,*/*;q=0.1")
	b.ReportAllocs()
	for b.Loop() {
		router.ServeHTTP(httptest.NewRecorder(), req)
	}
}

func BenchmarkProtocolsDecompress(b *testing.B) {
	for _, coding := range []string{"gzip", "deflate", "br"} {
		b.Run(coding, func(b *testing.B) {
			payload := bytes.Repeat([]byte("x"), maxBodyBytes)
			compressed := protocolCompressed(b, coding, payload)
			router := protocolTestRouter()
			b.SetBytes(int64(len(payload)))
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				req := httptest.NewRequest("POST", "/decompress/"+coding, bytes.NewReader(compressed))
				router.ServeHTTP(httptest.NewRecorder(), req)
			}
		})
	}
}
