package routes

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/TrueFix/httptoy/pkg/httpx"
	"github.com/gorilla/mux"
)

const maxBodyBytes = 1 << 20

func addExtendedRoutes(r *mux.Router) {
	methods := []string{"GET", "POST", "PUT", "PATCH", "HEAD", "OPTIONS", "DELETE", "CONNECT", "TRACE"}
	r.HandleFunc("/anything", anything).Methods(methods...)
	r.HandleFunc("/anything/{path:.*}", anything).Methods(methods...)
	r.HandleFunc("/response-headers", responseHeaders).Methods("GET")
	r.HandleFunc("/redirect-to", redirectTo).Methods("GET")
	r.HandleFunc("/stream/{count}", streamJSON).Methods("GET")
	r.HandleFunc("/stream-bytes/{count}", streamBytes).Methods("GET")
	r.HandleFunc("/sse", serverSentEvents).Methods("GET")
	r.HandleFunc("/drip", drip).Methods("GET")
	r.HandleFunc("/ws", websocketEcho).Methods("GET")
	r.HandleFunc("/trailers", trailers).Methods("GET")
	r.HandleFunc("/cache/{seconds}", cacheResponse).Methods("GET")
	r.HandleFunc("/etag/{value}", etagResponse).Methods("GET")
	r.HandleFunc("/range/{size}", rangeResponse).Methods("GET")
	r.HandleFunc("/healthz", func(w http.ResponseWriter, req *http.Request) {
		httpx.JSON(w, http.StatusOK, map[string]any{"status": "ok"})
	}).Methods("GET")
	r.HandleFunc("/links/{count}", links).Methods("GET")
	r.HandleFunc("/robots.txt", func(w http.ResponseWriter, req *http.Request) {
		httpx.Data(w, http.StatusOK, CONTENT_TEXT, []byte("User-agent: *\nDisallow: /deny\n"))
	}).Methods("GET").Name("robots")
	r.HandleFunc("/upload", upload).Methods("POST")
	r.HandleFunc("/auth/digest", digestAuth).Methods("GET")
}

func boundedNumber(value string, fallback, min, max int) (int, error) {
	if value == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < min || n > max {
		return 0, fmt.Errorf("value must be an integer between %d and %d", min, max)
	}
	return n, nil
}

func invalidRequest(w http.ResponseWriter, message string) {
	httpx.JSON(w, http.StatusBadRequest, map[string]any{"error": message})
}

func defaultQuery(req *http.Request, key, fallback string) string {
	values := req.URL.Query()
	if values.Has(key) {
		return values.Get(key)
	}
	return fallback
}

func validToken(value string) bool {
	if value == "" {
		return false
	}
	for _, ch := range value {
		if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", ch) {
			continue
		}
		return false
	}
	return true
}

func validHeaderValue(value string) bool {
	for _, ch := range value {
		if ch < 32 && ch != '\t' || ch == 127 {
			return false
		}
	}
	return true
}
