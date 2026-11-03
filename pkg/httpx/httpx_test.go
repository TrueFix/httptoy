package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResponses(t *testing.T) {
	w := httptest.NewRecorder()
	JSON(w, http.StatusCreated, map[string]any{"html": "<script>"})
	if w.Code != 201 || w.Header().Get("Content-Type") != "application/json; charset=utf-8" || strings.Contains(w.Body.String(), "<script>") {
		t.Fatalf("JSON: %v", w)
	}
	var value map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &value); err != nil || value["html"] != "<script>" {
		t.Fatalf("JSON payload: %v %v", value, err)
	}
	w = httptest.NewRecorder()
	JSON(w, 200, make(chan int))
	if w.Code != 500 || w.Body.Len() != 0 {
		t.Fatal("encoding error must not commit a 200")
	}
	w = httptest.NewRecorder()
	w.Header().Set("Content-Type", "custom/type")
	Data(w, 200, "text/plain", []byte("hello"))
	if w.Code != 200 || w.Body.String() != "hello" || w.Header().Get("Content-Type") != "custom/type" {
		t.Fatal("Data overwrote explicit type")
	}
}

func TestTrustedProxies(t *testing.T) {
	for _, tc := range []struct {
		name, peer, forwarded, real, want string
		proxies                           []string
	}{
		{"untrusted", "192.0.2.1:1000", "203.0.113.1", "", "192.0.2.1", nil},
		{"trusted", "192.0.2.1:1000", "203.0.113.1", "", "203.0.113.1", []string{"192.0.2.1"}},
		{"chain", "192.0.2.1:1000", "203.0.113.1, 192.0.2.2", "", "203.0.113.1", []string{"192.0.2.0/24"}},
		{"stop at untrusted", "192.0.2.1:1000", "203.0.113.99, 198.51.100.1, 192.0.2.2", "", "198.51.100.1", []string{"192.0.2.0/24"}},
		{"invalid chain", "192.0.2.1:1000", "invalid, 192.0.2.2", "", "192.0.2.1", []string{"192.0.2.0/24"}},
		{"real IP fallback", "192.0.2.1:1000", "", "203.0.113.2", "203.0.113.2", []string{"192.0.2.0/24"}},
		{"ignore untrusted real", "192.0.2.1:1000", "", "203.0.113.2", "192.0.2.1", nil},
		{"IPv6", "[2001:db8::1]:1000", "2001:db8:1::1", "", "2001:db8:1::1", []string{"2001:db8::/64"}},
		{"mapped IPv4", "[::ffff:192.0.2.1]:1000", "203.0.113.1", "", "203.0.113.1", []string{"192.0.2.0/24"}},
		{"peer without port", "192.0.2.1", "", "", "192.0.2.1", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			middleware, err := TrustedProxies(tc.proxies)
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tc.peer
			req.Header.Set("X-Forwarded-For", tc.forwarded)
			req.Header.Set("X-Real-IP", tc.real)
			w := httptest.NewRecorder()
			middleware(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { _, _ = w.Write([]byte(ClientIP(req))) })).ServeHTTP(w, req)
			if w.Body.String() != tc.want {
				t.Fatalf("IP=%q want=%q", w.Body.String(), tc.want)
			}
		})
	}
	for _, value := range []string{"", "invalid", "192.0.2.1/999"} {
		if _, err := TrustedProxies([]string{value}); err == nil {
			t.Fatalf("invalid proxy %q accepted", value)
		}
	}
}
