package routes

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"crypto/sha512"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/andybalholm/brotli"
)

func TestFaultStatuses(t *testing.T) {
	r := fixtureRouter()
	for _, tc := range []struct {
		path   string
		status int
	}{
		{"/unstable?failure_rate=0", 200}, {"/unstable?failure_rate=100", 503},
		{"/unstable?failure_rate=-1", 400}, {"/unstable?failure_rate=101", 400}, {"/unstable?failure_rate=bad", 400},
		{"/weighted-status?codes=201:1", 201}, {"/weighted-status?codes=429:1", 429},
		{"/weighted-status?codes=204:1", 204}, {"/weighted-status?codes=205:1", 205}, {"/weighted-status?codes=304:1", 304},
		{"/weighted-status?codes=200:0", 400}, {"/weighted-status?codes=103:1", 400},
		{"/weighted-status?codes=999:1", 400}, {"/weighted-status?codes=200:10001", 400},
		{"/weighted-status?codes=200:x", 400}, {"/weighted-status?codes=200:1,", 400},
	} {
		w := extendedRequest(r, "GET", tc.path, nil, nil)
		if w.Code != tc.status {
			t.Fatalf("%s: %d", tc.path, w.Code)
		}
		if tc.status == 204 || tc.status == 205 || tc.status == 304 {
			if w.Body.Len() != 0 {
				t.Fatal("body forbidden for status")
			}
		}
	}
	codes, total, ok := parseWeights("200:2,500:3,429:1")
	if !ok || total != 6 {
		t.Fatal("weight parsing failed")
	}
	for roll, want := range []int{200, 200, 500, 500, 500, 429} {
		if got := chooseStatus(codes, roll); got != want {
			t.Fatalf("roll %d=%d want %d", roll, got, want)
		}
	}
	if _, _, ok := parseWeights(strings.Repeat("200:1,", 17) + "200:1"); ok {
		t.Fatal("too many weights accepted")
	}
	if _, _, ok := parseWeights(strings.Repeat("200:10000,", 10) + "200:10000"); ok {
		t.Fatal("excess total weight accepted")
	}
}

func TestRetryAfter(t *testing.T) {
	r := fixtureRouter()
	w := extendedRequest(r, "GET", "/retry-after/20", nil, nil)
	if w.Code != 429 || w.Header().Get("Retry-After") != "20" {
		t.Fatalf("retry: %v", w)
	}
	before := time.Now().Add(20 * time.Second).Truncate(time.Second)
	w = extendedRequest(r, "GET", "/retry-after/20?format=date&status=503", nil, nil)
	date, err := http.ParseTime(w.Header().Get("Retry-After"))
	if err != nil || w.Code != 503 || date.Before(before) || date.After(before.Add(2*time.Second)) {
		t.Fatalf("date retry: %v %v", w, err)
	}
	for _, path := range []string{"/retry-after/-1", "/retry-after/86401", "/retry-after/1?format=invalid", "/retry-after/1?status=500"} {
		if w := extendedRequest(r, "GET", path, nil, nil); w.Code != 400 {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}
}

func TestLargeHeaders(t *testing.T) {
	r := fixtureRouter()
	w := extendedRequest(r, "GET", "/large-headers?count=4&size=30", nil, nil)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	for i := 0; i < 4; i++ {
		if got := w.Header().Get(fmt.Sprintf("X-Test-Header-%d", i)); got != strings.Repeat("x", 30) {
			t.Fatal("wrong header payload")
		}
	}
	for _, path := range []string{"/large-headers?count=65", "/large-headers?size=4097", "/large-headers?count=64&size=4096", "/large-headers?size=-1"} {
		if w := extendedRequest(r, "GET", path, nil, nil); w.Code != 400 {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}
}

func TestConfigurableCookies(t *testing.T) {
	r := fixtureRouter()
	query := url.Values{"first": {"one"}, "second": {"two"}, "secure": {"true"}, "httponly": {"true"}, "samesite": {"none"}, "max_age": {"3600"}, "expires": {"2030-01-02T03:04:05Z"}, "path": {"/test"}}
	w := extendedRequest(r, "GET", "/cookies/set?"+query.Encode(), nil, nil)
	cookies := w.Result().Cookies()
	if w.Code != 200 || len(cookies) != 2 {
		t.Fatalf("cookies: %d %s", w.Code, w.Body)
	}
	for _, cookie := range cookies {
		if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteNoneMode || cookie.MaxAge != 3600 || cookie.Path != "/test" || cookie.Expires.Year() != 2030 {
			t.Fatalf("attributes: %+v", cookie)
		}
	}
	for _, query := range []string{"", "a=b&secure=bad", "a=b&httponly=bad", "a=b&samesite=none", "a=b&samesite=bad", "a=b&expires=bad", "a=b&path=relative", "a=b&max_age=31536001", "a=b&domain=bad%3Bdomain", "a=hello%0d%0aBad", "__Host-test=a&secure=true&path=/other", "__Secure-test=a", "bad%20name=value"} {
		w := extendedRequest(r, "GET", "/cookies/set?"+query, nil, nil)
		if w.Code != 400 || len(w.Header().Values("Set-Cookie")) != 0 {
			t.Fatalf("invalid cookies %q: %d", query, w.Code)
		}
	}
	w = extendedRequest(r, "GET", "/cookies/set?a=b&max_age=-1", nil, nil)
	if w.Code != 200 || w.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("cookie deletion option")
	}
	w = extendedRequest(r, "GET", "/cookies/inspect", map[string]string{"Cookie": "a=one; a=two; b=three"}, nil)
	var got struct {
		Raw     []string                       `json:"raw"`
		Cookies []struct{ Name, Value string } `json:"cookies"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || len(got.Raw) != 1 || len(got.Cookies) != 3 || got.Cookies[1].Value != "two" {
		t.Fatalf("cookie inspection: %s", w.Body)
	}
	tooMany := url.Values{}
	for i := 0; i < 17; i++ {
		tooMany.Set(fmt.Sprintf("c%d", i), "v")
	}
	if w := extendedRequest(r, "GET", "/cookies/set?"+tooMany.Encode(), nil, nil); w.Code != 400 {
		t.Fatal("cookie count limit ignored")
	}
}

func TestMultipartResponse(t *testing.T) {
	r := fixtureRouter()
	w := extendedRequest(r, "POST", "/multipart/response", nil, bytes.NewReader([]byte{0, 1, 255}))
	media, params, err := mime.ParseMediaType(w.Header().Get("Content-Type"))
	if err != nil || media != "multipart/mixed" || w.Code != 200 {
		t.Fatalf("multipart: %v %v", w, err)
	}
	reader := multipart.NewReader(w.Body, params["boundary"])
	part, err := reader.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	var metadata struct {
		Bytes  int    `json:"bytes"`
		SHA256 string `json:"sha256"`
	}
	if err := json.NewDecoder(part).Decode(&metadata); err != nil {
		t.Fatal(err)
	}
	part, err = reader.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(part)
	if err != nil || !bytes.Equal(body, []byte{0, 1, 255}) || metadata.Bytes != 3 || metadata.SHA256 != checksum(body) || part.FileName() != "echo.bin" {
		t.Fatal("multipart payload mismatch")
	}
	if _, err := reader.NextPart(); err != io.EOF {
		t.Fatalf("multipart terminator: %v", err)
	}
	for _, path := range []string{"/multipart/response", "/checksum/sha256", "/checksum/sha512"} {
		if w := extendedRequest(r, "POST", path, nil, strings.NewReader(strings.Repeat("x", maxBodyBytes+1))); w.Code != 413 {
			t.Fatalf("%s limit: %d", path, w.Code)
		}
	}
}

func TestCharsets(t *testing.T) {
	r := fixtureRouter()
	for _, charset := range []string{"utf-8", "utf-16", "utf-16le", "utf-16be", "iso-8859-1"} {
		w := extendedRequest(r, "GET", "/encoding/"+charset, nil, nil)
		if w.Code != 200 || w.Header().Get("Content-Type") != "text/plain; charset="+charset {
			t.Fatalf("charset %s: %v", charset, w)
		}
		data := w.Body.Bytes()
		decoded := ""
		switch charset {
		case "utf-8":
			decoded = string(data)
		case "iso-8859-1":
			for _, ch := range data {
				decoded += string(rune(ch))
			}
		default:
			var order binary.ByteOrder = binary.BigEndian
			if charset == "utf-16le" {
				order = binary.LittleEndian
			}
			if charset == "utf-16" {
				if !bytes.HasPrefix(data, []byte{0xfe, 0xff}) {
					t.Fatal("UTF-16 missing BOM")
				}
				data = data[2:]
			}
			units := make([]uint16, 0, len(data)/2)
			for i := 0; i < len(data); i += 2 {
				units = append(units, order.Uint16(data[i:i+2]))
			}
			decoded = string(utf16.Decode(units))
		}
		if decoded != charsetSample {
			t.Fatalf("decoded %s=%q", charset, decoded)
		}
	}
	if w := extendedRequest(r, "GET", "/encoding/unknown", nil, nil); w.Code != 400 {
		t.Fatal("unsupported charset accepted")
	}
}

func TestCompressionNegotiation(t *testing.T) {
	r := fixtureRouter()
	for _, tc := range []struct {
		accept, encoding string
		status           int
	}{
		{"", "", 200}, {"gzip", "gzip", 200}, {"br, gzip", "br", 200},
		{"gzip;q=0.9,br;q=0.8,identity;q=0", "gzip", 200}, {"deflate;q=1,identity;q=0", "deflate", 200},
		{"*;q=1", "br", 200}, {"*;q=0", "", 406}, {"*;q=0,identity;q=1", "", 200},
		{"gzip;q=0.5", "", 200}, {"zstd", "", 200}, {"identity;q=0,br;q=0,gzip;q=0,deflate;q=0", "", 406},
		{"gzip;q=NaN", "", 400}, {"gzip;q=1.001", "", 400}, {"gzip;q=0.9999", "", 400}, {"gzip;q=-1", "", 400},
		{"gzip;q=0.2;q=0.3", "", 400}, {"gzip;q=.5", "", 400},
	} {
		w := extendedRequest(r, "GET", "/compression/negotiate", map[string]string{"Accept-Encoding": tc.accept}, nil)
		if w.Code != tc.status || w.Header().Get("Content-Encoding") != tc.encoding || w.Header().Get("Vary") != "Accept-Encoding" {
			t.Fatalf("%q: status=%d encoding=%q", tc.accept, w.Code, w.Header().Get("Content-Encoding"))
		}
		if w.Code != 200 {
			continue
		}
		var reader io.Reader = w.Body
		switch tc.encoding {
		case "br":
			reader = brotli.NewReader(reader)
		case "gzip":
			decoded, err := gzip.NewReader(reader)
			if err != nil {
				t.Fatal(err)
			}
			defer decoded.Close()
			reader = decoded
		case "deflate":
			decoded, err := zlib.NewReader(reader)
			if err != nil {
				t.Fatal(err)
			}
			defer decoded.Close()
			reader = decoded
		}
		body, err := io.ReadAll(reader)
		if err != nil || string(body) != DEFAULT_STRING {
			t.Fatalf("decoded body %q error=%v", body, err)
		}
	}
}

func TestChecksumsTimeAndDownloads(t *testing.T) {
	r := fixtureRouter()
	for _, algorithm := range []string{"sha256", "sha512", "sha-256", "sha-512"} {
		w := extendedRequest(r, "POST", "/checksum/"+algorithm, nil, strings.NewReader("hello"))
		var got struct {
			Bytes    int    `json:"bytes"`
			Checksum string `json:"checksum"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		want := checksum([]byte("hello"))
		if strings.Contains(algorithm, "512") {
			sum := sha512.Sum512([]byte("hello"))
			want = hex.EncodeToString(sum[:])
		}
		if w.Code != 200 || got.Bytes != 5 || got.Checksum != want {
			t.Fatalf("checksum: %s", w.Body)
		}
	}
	if w := extendedRequest(r, "POST", "/checksum/md5", nil, nil); w.Code != 400 {
		t.Fatal("unsupported checksum accepted")
	}
	w := extendedRequest(r, "GET", "/time", nil, nil)
	var now struct {
		RFC3339 string `json:"rfc3339"`
		Unix    int64  `json:"unix"`
		Millis  int64  `json:"unix_ms"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &now); err != nil {
		t.Fatal(err)
	}
	parsed, err := time.Parse(time.RFC3339Nano, now.RFC3339)
	if err != nil || parsed.Unix() != now.Unix || parsed.UnixMilli() != now.Millis || time.Since(parsed) > time.Second {
		t.Fatalf("time: %s", w.Body)
	}
	for _, name := range []string{"report.txt", "résumé.txt", "report \"2026\".txt"} {
		w := extendedRequest(r, "GET", "/download/"+url.PathEscape(name)+"?size=26", nil, nil)
		disposition, params, err := mime.ParseMediaType(w.Header().Get("Content-Disposition"))
		if err != nil || w.Code != 200 || disposition != "attachment" || params["filename"] != name || w.Body.String() != "abcdefghijklmnopqrstuvwxyz" {
			t.Fatalf("download %q: %v", name, w)
		}
	}
	for _, path := range []string{"/download/file?size=1048577", "/download/file?disposition=bad", "/download/a%0d%0ab", "/download/a%5cb", "/download/.."} {
		if w := extendedRequest(r, "GET", path, nil, nil); w.Code != 400 {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}
}

func TestConnectionInfo(t *testing.T) {
	req := httptest.NewRequest("GET", "/connection", nil)
	req.RemoteAddr = "192.0.2.1:1234"
	w := httptest.NewRecorder()
	fixtureRouter().ServeHTTP(w, req)
	var got struct {
		Version string `json:"http_version"`
		Peer    string `json:"peer_address"`
		IP      string `json:"client_ip"`
		TLS     any    `json:"tls"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || got.Version != "HTTP/1.1" || got.Peer != req.RemoteAddr || got.IP != "192.0.2.1" || got.TLS != nil {
		t.Fatalf("connection: %s", w.Body)
	}
}

func TestNewRouteContract(t *testing.T) {
	registered := registeredRoutes(t, fixtureRouter())
	for _, path := range []string{"/unstable", "/retry-after/{seconds}", "/weighted-status", "/abort", "/early-hints", "/informational/{code}", "/large-headers", "/cookies/set", "/cookies/inspect", "/encoding/{charset}", "/compression/negotiate", "/connection", "/time", "/download/{filename}"} {
		if !registered["GET "+path] {
			t.Fatalf("missing GET %s", path)
		}
	}
	for _, path := range []string{"/multipart/response", "/checksum/{algorithm}"} {
		if !registered["POST "+path] {
			t.Fatalf("missing POST %s", path)
		}
	}
}

func TestProtocolParameterValidation(t *testing.T) {
	r := fixtureRouter()
	for _, path := range []string{"/abort?after_bytes=-1", "/abort?after_bytes=65537", "/early-hints?link=x%0d%0aBad:yes", "/informational/101", "/informational/103", "/informational/500"} {
		if w := extendedRequest(r, "GET", path, nil, nil); w.Code != 400 {
			t.Fatalf("invalid %s: %d", path, w.Code)
		}
	}
}
