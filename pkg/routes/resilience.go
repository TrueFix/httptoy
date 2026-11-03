package routes

import (
	"crypto/rand"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/TrueFix/httptoy/pkg/httpx"
	"github.com/gorilla/mux"
)

func addResilienceRoutes(r *mux.Router) {
	r.HandleFunc("/unstable", unstable).Methods("GET")
	r.HandleFunc("/retry-after/{seconds}", retryAfter).Methods("GET")
	r.HandleFunc("/weighted-status", weightedStatus).Methods("GET")
	r.HandleFunc("/abort", abortResponse).Methods("GET")
	r.HandleFunc("/early-hints", earlyHints).Methods("GET")
	r.HandleFunc("/informational/{code}", informational).Methods("GET")
	r.HandleFunc("/large-headers", largeHeaders).Methods("GET")
	r.HandleFunc("/cookies/set", configurableCookies).Methods("GET")
	r.HandleFunc("/cookies/inspect", inspectCookies).Methods("GET")
	r.HandleFunc("/multipart/response", multipartResponse).Methods("POST")
	r.HandleFunc("/encoding/{charset}", charsetResponse).Methods("GET")
	r.HandleFunc("/compression/negotiate", negotiateCompression).Methods("GET")
	r.HandleFunc("/connection", connectionInfo).Methods("GET")
	r.HandleFunc("/checksum/{algorithm}", requestChecksum).Methods("POST")
	r.HandleFunc("/time", serverTime).Methods("GET")
	r.HandleFunc("/download/{filename}", download).Methods("GET")
}

func randomBelow(max int) (int, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		return 0, err
	}
	return int(n.Int64()), nil
}

func unstable(w http.ResponseWriter, req *http.Request) {
	rate, err := boundedNumber(req.URL.Query().Get("failure_rate"), 30, 0, 100)
	if err != nil {
		invalidRequest(w, err.Error())
		return
	}
	roll, err := randomBelow(100)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	status := http.StatusOK
	if roll < rate {
		status = http.StatusServiceUnavailable
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, status, map[string]any{"status": status, "failure_rate": rate})
}

func retryAfter(w http.ResponseWriter, req *http.Request) {
	seconds, err := boundedNumber(mux.Vars(req)["seconds"], 0, 0, 86400)
	if err != nil {
		invalidRequest(w, err.Error())
		return
	}
	status, err := boundedNumber(req.URL.Query().Get("status"), 429, 429, 503)
	if err != nil || status != 429 && status != 503 {
		invalidRequest(w, "status must be 429 or 503")
		return
	}
	value := strconv.Itoa(seconds)
	switch defaultQuery(req, "format", "seconds") {
	case "seconds":
	case "date":
		value = time.Now().UTC().Add(time.Duration(seconds) * time.Second).Format(http.TimeFormat)
	default:
		invalidRequest(w, "format must be seconds or date")
		return
	}
	w.Header().Set("Retry-After", value)
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, status, map[string]any{"status": status, "retry_after": value})
}

type weightedCode struct{ code, weight int }

func validFinalStatus(code int) bool {
	return code >= 200 && code <= 599 && http.StatusText(code) != ""
}

func finalStatusCodes() []int {
	codes := make([]int, 0)
	for code := 200; code <= 599; code++ {
		if validFinalStatus(code) {
			codes = append(codes, code)
		}
	}
	return codes
}

func parseWeights(value string) ([]weightedCode, int, bool) {
	if len(value) > 1024 {
		return nil, 0, false
	}
	entries := strings.Split(value, ",")
	if len(entries) > 16 {
		return nil, 0, false
	}
	codes := make([]weightedCode, 0, len(entries))
	total := 0
	for _, entry := range entries {
		codeString, weightString, found := strings.Cut(entry, ":")
		code, err := strconv.Atoi(strings.TrimSpace(codeString))
		if !found || err != nil || !validFinalStatus(code) {
			return nil, 0, false
		}
		weight, err := strconv.Atoi(strings.TrimSpace(weightString))
		if err != nil || weight < 1 || weight > 10000 {
			return nil, 0, false
		}
		total += weight
		if total > 100000 {
			return nil, 0, false
		}
		codes = append(codes, weightedCode{code, weight})
	}
	return codes, total, true
}

func chooseStatus(codes []weightedCode, roll int) int {
	for _, item := range codes {
		if roll < item.weight {
			return item.code
		}
		roll -= item.weight
	}
	return http.StatusInternalServerError
}

func weightedStatus(w http.ResponseWriter, req *http.Request) {
	codes, total, ok := parseWeights(defaultQuery(req, "codes", "200:80,500:20"))
	if !ok {
		invalidRequest(w, "codes must contain up to 16 recognized 200–599 status:weight pairs; weights 1–10000, total <=100000")
		return
	}
	roll, err := randomBelow(total)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	status := chooseStatus(codes, roll)
	w.Header().Set("Cache-Control", "no-store")
	if status == http.StatusNoContent || status == http.StatusResetContent || status == http.StatusNotModified {
		w.WriteHeader(status)
		return
	}
	httpx.JSON(w, status, map[string]any{"status": status})
}

func abortResponse(w http.ResponseWriter, req *http.Request) {
	count, err := boundedNumber(req.URL.Query().Get("after_bytes"), 100, 0, 65536)
	if err != nil {
		invalidRequest(w, err.Error())
		return
	}
	ctx, cancel, ok := startStream(w, req)
	if !ok {
		return
	}
	defer cancel()
	if ctx.Err() != nil {
		return
	}
	w.Header().Set("Content-Type", CONTENT_BYTES)
	w.Header().Set("Cache-Control", "no-store")
	// Advertise one extra byte so even HTTP/1 clients can detect truncation.
	w.Header().Set("Content-Length", strconv.Itoa(count+1))
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(strings.Repeat("*", count))); err != nil {
		return
	}
	if err := http.NewResponseController(w).Flush(); err != nil {
		return
	}
	// net/http closes HTTP/1 connections or resets HTTP/2 streams. Do not
	// hijack: doing so would incorrectly close unrelated HTTP/2 streams.
	panic(http.ErrAbortHandler)
}

func earlyHints(w http.ResponseWriter, req *http.Request) {
	link := defaultQuery(req, "link", "</image/png>; rel=preload; as=image")
	if len(link) > 1024 || !validHeaderValue(link) || link == "" {
		invalidRequest(w, "invalid Link header")
		return
	}
	w.Header().Set("Link", link)
	w.WriteHeader(http.StatusEarlyHints)
	w.Header().Del("Link")
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, map[string]any{"informational": 103, "status": 200})
}

func informational(w http.ResponseWriter, req *http.Request) {
	code, err := strconv.Atoi(mux.Vars(req)["code"])
	if err != nil || code != 100 && code != 102 {
		invalidRequest(w, "code must be 100 or 102; use /early-hints for 103")
		return
	}
	w.WriteHeader(code)
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, map[string]any{"informational": code, "status": 200})
}

func largeHeaders(w http.ResponseWriter, req *http.Request) {
	count, err := boundedNumber(req.URL.Query().Get("count"), 10, 0, 64)
	if err != nil {
		invalidRequest(w, err.Error())
		return
	}
	size, err := boundedNumber(req.URL.Query().Get("size"), 1024, 0, 4096)
	if err != nil || count*size > 65536 {
		invalidRequest(w, "size must be 0–4096 and total generated header values <=65536 bytes")
		return
	}
	value := strings.Repeat("x", size)
	for i := 0; i < count; i++ {
		w.Header().Set("X-Test-Header-"+strconv.Itoa(i), value)
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, map[string]any{"count": count, "size": size})
}
