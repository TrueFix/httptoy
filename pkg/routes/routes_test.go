package routes

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	samplefiles "github.com/TrueFix/httptoy/sample_files"
	"github.com/gorilla/mux"
)

func fixtureRouter() *mux.Router {
	r := NewRouter()
	AddRoutes(r)
	return r
}

func registeredRoutes(t *testing.T, r *mux.Router) map[string]bool {
	t.Helper()
	registered := make(map[string]bool)
	err := r.Walk(func(route *mux.Route, router *mux.Router, ancestors []*mux.Route) error {
		path, err := route.GetPathTemplate()
		if err != nil {
			return err
		}
		methods, err := route.GetMethods()
		if err != nil {
			return err
		}
		for _, method := range methods {
			registered[method+" "+path] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return registered
}

func TestFixtures(t *testing.T) {
	r := fixtureRouter()
	for _, tc := range []struct{ path, file, mime string }{
		{"/text", "data/sample.text", CONTENT_TEXT},
		{"/json", "data/sample.json", CONTENT_JSON},
		{"/xml", "data/sample.xml", CONTENT_XML},
		{"/yaml", "data/sample.yaml", CONTENT_YAML},
		{"/html", "data/sample.html", CONTENT_HTML},
		{"/image/png", "images/sample.png", CONTENT_IMAGE_PNG},
		{"/image/jpeg", "images/sample.jpg", CONTENT_IMAGE_JPEG},
		{"/image/svg", "images/sample.svg", CONTENT_IMAGE_SVG},
		{"/image/webp", "images/sample.webp", CONTENT_IMAGE_WEBP},
	} {
		t.Run(tc.path, func(t *testing.T) {
			want, err := samplefiles.Files.ReadFile(tc.file)
			if err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
			if w.Code != 200 || w.Header().Get("Content-Type") != tc.mime || !bytes.Equal(w.Body.Bytes(), want) {
				t.Fatalf("status=%d mime=%q size=%d want=%d", w.Code, w.Header().Get("Content-Type"), w.Body.Len(), len(want))
			}
		})
	}
	for _, accept := range []string{"image/jpeg", "image/png;q=0,image/jpeg;q=1"} {
		req := httptest.NewRequest("GET", "/image", nil)
		req.Header.Set("Accept", accept)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Header().Get("Content-Type") != CONTENT_IMAGE_JPEG || w.Body.Len() == 0 {
			t.Fatalf("JPEG negotiation failed: %v", w)
		}
	}
	req := httptest.NewRequest("GET", "/image", nil)
	req.Header.Set("Accept", "image/webp;q=0,image/svg+xml;q=0,image/jpeg;q=0,image/png;q=0,image/*;q=1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Header().Get("Content-Type") == CONTENT_IMAGE_PNG {
		t.Fatal("served explicitly forbidden PNG")
	}
}

func TestBytesBoundaries(t *testing.T) {
	r := fixtureRouter()
	for _, tc := range []struct {
		count          string
		status, length int
	}{
		{"-1", 400, 0}, {"0", 200, 0}, {"1", 200, 1}, {"1024", 200, 1024}, {"1025", 200, 1024}, {"bad", 200, 1024},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/bytes/"+tc.count, nil))
		if w.Code != tc.status || w.Body.Len() != tc.length {
			t.Fatalf("%s: status=%d length=%d", tc.count, w.Code, w.Body.Len())
		}
	}
}

func TestBearerValidation(t *testing.T) {
	r := fixtureRouter()
	for _, tc := range []struct {
		auth   string
		status int
	}{
		{"", 401}, {"Bearer", 401}, {"BearerNotAToken", 401}, {"Bearer ", 401},
		{"Bearer a b", 401}, {"Bearer =", 401}, {"Bearer a=b", 401},
		{"bearer abc", 200}, {"Bearer abc+/==", 200},
	} {
		req := httptest.NewRequest("GET", "/bearer", nil)
		req.Header.Set("Authorization", tc.auth)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Fatalf("%q: status=%d", tc.auth, w.Code)
		}
		if tc.status == 401 && w.Header().Get("WWW-Authenticate") != "Bearer" {
			t.Fatal("missing auth challenge")
		}
	}
}

func TestDelays(t *testing.T) {
	for value, want := range map[string]int{"-1": -1, "0": 0, "bad": 0, "11": 10, "9223372036854775807": 10} {
		if got := delaySeconds(value); got != want {
			t.Fatalf("%s: %d", value, got)
		}
	}
	r := fixtureRouter()
	for _, path := range []string{"/delay/-1", "/redirect-delay/-1/2"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 400 {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}
	for _, path := range []string{"/delay/10", "/redirect-delay/10/2"} {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", path, nil).WithContext(ctx))
		}()
		// Cancel after the handler has had a chance to enter the timer wait.
		timer := time.AfterFunc(20*time.Millisecond, cancel)
		select {
		case <-done:
		case <-time.After(time.Second):
			cancel()
			t.Fatal("delay ignored cancellation")
		}
		timer.Stop()
		cancel()
	}
	if len(delaySlots) != 0 {
		t.Fatal("leaked admission slots")
	}
	for i := 0; i < cap(delaySlots); i++ {
		delaySlots <- struct{}{}
	}
	defer func() {
		for len(delaySlots) > 0 {
			<-delaySlots
		}
	}()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/delay/1", nil))
	if w.Code != 503 || w.Header().Get("Retry-After") == "" {
		t.Fatalf("overload response=%d", w.Code)
	}
}

func TestRouteContract(t *testing.T) {
	r := fixtureRouter()
	registered := registeredRoutes(t, r)
	for _, method := range []string{"GET", "POST", "DELETE", "PUT", "PATCH"} {
		for _, path := range []string{"/delay/{seconds}", "/status/{code}"} {
			if !registered[method+" "+path] {
				t.Fatalf("missing %s %s", method, path)
			}
		}
	}
	if !registered["PATCH /patch"] || registered["PATCH /delete"] {
		t.Fatal("incorrect PATCH routes")
	}
	for _, method := range []string{"OPTIONS", "HEAD"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(method, "/delay/0", nil))
		if w.Code != http.StatusNotFound {
			t.Fatal("unexpected method supported")
		}
	}
}

func TestRedirectTargets(t *testing.T) {
	r := fixtureRouter()
	for times := 0; times < 4; times++ {
		for _, prefix := range []string{"/redirect/", "/redirect-delay/0/"} {
			want := "/get"
			if times > 1 {
				want = prefix + strconv.Itoa(times-1)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", prefix+strconv.Itoa(times), nil))
			if w.Code != 302 || w.Header().Get("Location") != want {
				t.Fatalf("redirect: %v", w)
			}
		}
	}
}

func BenchmarkFixtures(b *testing.B) {
	r := fixtureRouter()
	for _, path := range []string{"/get", "/text", "/image/png"} {
		b.Run(path, func(b *testing.B) {
			req := httptest.NewRequest("GET", path, nil)
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				r.ServeHTTP(httptest.NewRecorder(), req)
			}
		})
	}
}

func TestMuxPathBehavior(t *testing.T) {
	r := fixtureRouter()
	for _, tc := range []struct {
		method, path, location string
		status                 int
	}{
		{"GET", "/get/?q=1", "/get?q=1", 301},
		{"POST", "/post/", "/post", 307},
		{"GET", "/does-not-exist/", "", 404},
		{"POST", "/get/", "", 404},
		{"HEAD", "/get", "", 404},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != tc.status || w.Header().Get("Location") != tc.location {
			t.Fatalf("%s %s: %d %q", tc.method, tc.path, w.Code, w.Header().Get("Location"))
		}
	}
	for _, path := range []string{"/anything/", "/anything/a//b", "/anything/a/../b", "/anything/a%2Fb"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || w.Header().Get("Location") != "" {
			t.Fatalf("testing path normalized: %s status=%d", path, w.Code)
		}
	}
}

func TestBasicAndCookieRoutes(t *testing.T) {
	r := fixtureRouter()
	for _, path := range []string{"/basic", "/basic/user/pass"} {
		req := httptest.NewRequest("GET", path, nil)
		req.SetBasicAuth("user", "pass")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("basic %s: %d", path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/cookies/set/session/hello%20world", nil))
	cookies := w.Result().Cookies()
	if w.Code != 302 || len(cookies) != 1 || cookies[0].Name != "session" || cookies[0].Value != "hello+world" || cookies[0].Path != "/" {
		t.Fatalf("set cookie: %v", w)
	}
	req := httptest.NewRequest("GET", "/cookies", nil)
	req.AddCookie(cookies[0])
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 || w.Body.String() != `{"cookies":{"session":"hello+world"}}` {
		t.Fatalf("cookies: %s", w.Body)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/cookies/delete/session", nil))
	cookies = w.Result().Cookies()
	if w.Code != 302 || len(cookies) != 1 || cookies[0].MaxAge != -1 {
		t.Fatal("delete cookie failed")
	}
}

func TestBase64SlashData(t *testing.T) {
	r := fixtureRouter()
	for _, tc := range []struct {
		path string
		want []byte
	}{
		{"/base64de/%2Fw==", []byte{0xff}},
		{"/base64de//w==", []byte{0xff}},
		{"/base64de/aGVsbG8=", []byte("hello")},
		{"/base64en/a%2Fb", []byte("YS9i")},
	} {
		w := extendedRequest(r, "GET", tc.path, nil, nil)
		if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), tc.want) {
			t.Fatalf("%s: status=%d body=%x", tc.path, w.Code, w.Body.Bytes())
		}
	}
}
