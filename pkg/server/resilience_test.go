package server

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/textproto"
	"strings"
	"testing"
)

func TestInformationalAndAbortProtocols(t *testing.T) {
	t.Setenv("TRUSTED_PROXIES", "")
	for _, useHTTP2 := range []bool{false, true} {
		name := "HTTP1"
		if useHTTP2 {
			name = "HTTP2"
		}
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewUnstartedServer(Server())
			srv.EnableHTTP2 = useHTTP2
			if useHTTP2 {
				srv.StartTLS()
			} else {
				srv.Start()
			}
			defer srv.Close()
			client := srv.Client()
			for _, tc := range []struct {
				path string
				code int
			}{
				{"/informational/100", 100}, {"/informational/102", 102}, {"/early-hints", 103},
			} {
				codes := make([]int, 0)
				link := ""
				trace := &httptrace.ClientTrace{Got1xxResponse: func(code int, header textproto.MIMEHeader) error {
					codes = append(codes, code)
					link = header.Get("Link")
					return nil
				}}
				req, err := http.NewRequest("GET", srv.URL+tc.path, nil)
				if err != nil {
					t.Fatal(err)
				}
				req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
				resp, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				if err != nil || resp.StatusCode != 200 || len(codes) != 1 || codes[0] != tc.code || !strings.Contains(string(body), `"status":200`) {
					t.Fatalf("%s: codes=%v final=%d body=%s error=%v", tc.path, codes, resp.StatusCode, body, err)
				}
				if tc.code == 103 && (link != "</image/png>; rel=preload; as=image" || resp.Header.Get("Link") != "") {
					t.Fatal("early hints header missing or leaked into final response")
				}
				if useHTTP2 && resp.ProtoMajor != 2 {
					t.Fatal("HTTP/2 not exercised")
				}
			}
			var beforeConn, afterConn net.Conn
			checkHealth := func(destination *net.Conn) {
				t.Helper()
				req, err := http.NewRequest("GET", srv.URL+"/healthz", nil)
				if err != nil {
					t.Fatal(err)
				}
				req = req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) { *destination = info.Conn }}))
				resp, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				_, err = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if err != nil || resp.StatusCode != 200 {
					t.Fatal("healthy request failed")
				}
			}
			checkHealth(&beforeConn)
			for _, suffix := range []string{"0", "100"} {
				resp, err := client.Get(srv.URL + "/abort?after_bytes=" + suffix)
				if err != nil {
					if !useHTTP2 {
						t.Fatal(err)
					}
					continue
				}
				body, readErr := io.ReadAll(resp.Body)
				resp.Body.Close()
				if resp.StatusCode != 200 || readErr == nil || len(body) > 100 {
					t.Fatalf("abort: status=%d len=%d error=%v", resp.StatusCode, len(body), readErr)
				}
				if !useHTTP2 && suffix == "100" && len(body) != 100 {
					t.Fatalf("expected 100 partial bytes, got %d", len(body))
				}
			}
			checkHealth(&afterConn)
			if useHTTP2 && beforeConn != afterConn {
				t.Fatal("aborting an HTTP/2 stream closed the connection")
			}
			resp, err := client.Get(srv.URL + "/connection")
			if err != nil {
				t.Fatal(err)
			}
			var info struct {
				Version string `json:"http_version"`
				TLS     *struct {
					Version   string `json:"version"`
					ALPN      string `json:"alpn"`
					Handshake bool   `json:"handshake_complete"`
				} `json:"tls"`
			}
			err = json.NewDecoder(resp.Body).Decode(&info)
			resp.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if useHTTP2 {
				if info.Version != "HTTP/2.0" || info.TLS == nil || info.TLS.ALPN != "h2" || !info.TLS.Handshake || info.TLS.Version == "" {
					t.Fatalf("TLS/ALPN details: %+v", info)
				}
			} else if info.Version != "HTTP/1.1" || info.TLS != nil {
				t.Fatal("plaintext connection misreported")
			}
		})
	}
}

func TestChunkedChecksumLimit(t *testing.T) {
	t.Setenv("TRUSTED_PROXIES", "")
	srv := httptest.NewServer(Server())
	defer srv.Close()
	// Wrapping the reader hides its length, so net/http sends a chunked upload.
	req, err := http.NewRequest("POST", srv.URL+"/checksum/sha256", io.NopCloser(strings.NewReader(strings.Repeat("x", (1<<20)+1))))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 413 {
		t.Fatalf("chunked checksum cap: %d", resp.StatusCode)
	}
}
