package routes

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/TrueFix/httptoy/pkg/httpx"
	"github.com/TrueFix/httptoy/pkg/parsers"
	"github.com/gorilla/mux"
)

var streamSlots = make(chan struct{}, 128)

func startStream(w http.ResponseWriter, req *http.Request) (context.Context, context.CancelFunc, bool) {
	select {
	case streamSlots <- struct{}{}:
		// Allow two seconds of transport overhead around at most ten seconds
		// of intentional wait, without allowing unbounded slow-client occupancy.
		ctx, cancel := context.WithTimeout(req.Context(), 12*time.Second)
		controller := http.NewResponseController(w)
		// Supported on live net/http connections; recorders may not implement it.
		_ = controller.SetWriteDeadline(time.Now().Add(12 * time.Second))
		return ctx, func() {
			cancel()
			<-streamSlots
		}, true
	default:
		streamRejections.Add(1)
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusServiceUnavailable)
		return nil, nil, false
	}
}

func pause(ctx context.Context, duration time.Duration) bool {
	if ctx.Err() != nil {
		return false
	}
	if duration <= 0 {
		return true
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func streamJSON(w http.ResponseWriter, req *http.Request) {
	if len(req.URL.RequestURI()) > 4096 {
		w.WriteHeader(http.StatusRequestURITooLong)
		return
	}
	count, err := boundedNumber(mux.Vars(req)["count"], 0, 0, 100)
	if err != nil {
		invalidRequest(w, err.Error())
		return
	}
	ctx, cancel, ok := startStream(w, req)
	if !ok {
		return
	}
	defer cancel()
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	encoder := json.NewEncoder(w)
	url, origin := parsers.ParseURL(req), httpx.ClientIP(req)
	for i := 0; i < count && ctx.Err() == nil; i++ {
		if err := encoder.Encode(map[string]any{"id": i, "url": url, "origin": origin}); err != nil {
			return
		}
		if err := http.NewResponseController(w).Flush(); err != nil {
			return
		}
	}
}

func streamBytes(w http.ResponseWriter, req *http.Request) {
	count, err := boundedNumber(mux.Vars(req)["count"], 0, 0, maxBodyBytes)
	if err != nil {
		invalidRequest(w, err.Error())
		return
	}
	chunkSize, err := boundedNumber(req.URL.Query().Get("chunk_size"), 4096, 1, 65536)
	if err != nil {
		invalidRequest(w, err.Error())
		return
	}
	ctx, cancel, ok := startStream(w, req)
	if !ok {
		return
	}
	defer cancel()
	w.Header().Set("Content-Type", CONTENT_BYTES)
	w.Header().Set("Cache-Control", "no-store")
	buffer := make([]byte, min(chunkSize, count))
	for remaining := count; remaining > 0 && ctx.Err() == nil; {
		chunk := buffer[:min(len(buffer), remaining)]
		if _, err := rand.Read(chunk); err != nil {
			return
		}
		if _, err := w.Write(chunk); err != nil {
			return
		}
		remaining -= len(chunk)
		if err := http.NewResponseController(w).Flush(); err != nil {
			return
		}
	}
}

func serverSentEvents(w http.ResponseWriter, req *http.Request) {
	count, err := boundedNumber(req.URL.Query().Get("count"), 5, 0, 100)
	if err != nil {
		invalidRequest(w, err.Error())
		return
	}
	interval, err := boundedNumber(req.URL.Query().Get("interval_ms"), 100, 0, 1000)
	if err != nil {
		invalidRequest(w, err.Error())
		return
	}
	if max(0, count-1)*interval > 10000 {
		invalidRequest(w, "total SSE wait cannot exceed 10 seconds")
		return
	}
	ctx, cancel, ok := startStream(w, req)
	if !ok {
		return
	}
	defer cancel()
	sseClients.Add(1)
	defer sseClients.Add(-1)
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	for i := 0; i < count; i++ {
		wait := time.Duration(interval) * time.Millisecond
		if i == 0 {
			wait = 0
		}
		if !pause(ctx, wait) {
			return
		}
		if _, err := fmt.Fprintf(w, "id: %d\nevent: message\ndata: {\"id\":%d}\n\n", i, i); err != nil {
			return
		}
		if err := http.NewResponseController(w).Flush(); err != nil {
			return
		}
	}
}

func drip(w http.ResponseWriter, req *http.Request) {
	count, err := boundedNumber(req.URL.Query().Get("numbytes"), 10, 1, 1024)
	if err != nil {
		invalidRequest(w, err.Error())
		return
	}
	duration, err := boundedNumber(req.URL.Query().Get("duration"), 1, 0, 10)
	if err != nil {
		invalidRequest(w, err.Error())
		return
	}
	delay, err := boundedNumber(req.URL.Query().Get("delay"), 0, 0, 10)
	if err != nil || duration+delay > 10 {
		invalidRequest(w, "delay plus duration must not exceed 10 seconds")
		return
	}
	ctx, cancel, ok := startStream(w, req)
	if !ok {
		return
	}
	defer cancel()
	if !pause(ctx, time.Duration(delay)*time.Second) {
		return
	}
	w.Header().Set("Content-Type", CONTENT_BYTES)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Content-Length", strconv.Itoa(count))
	started := time.Now()
	for i := 0; i < count; i++ {
		if count > 1 {
			// Schedule relative to the initial instant, avoiding accumulated drift.
			target := started.Add(time.Duration(duration) * time.Second * time.Duration(i) / time.Duration(count-1))
			if !pause(ctx, time.Until(target)) {
				return
			}
		}
		if ctx.Err() != nil {
			return
		}
		if _, err := w.Write([]byte{'*'}); err != nil {
			return
		}
		if err := http.NewResponseController(w).Flush(); err != nil {
			return
		}
	}
}

func trailers(w http.ResponseWriter, req *http.Request) {
	ctx, cancel, ok := startStream(w, req)
	if !ok {
		return
	}
	defer cancel()
	if ctx.Err() != nil {
		return
	}
	w.Header().Set("Content-Type", CONTENT_TEXT)
	w.Header().Set("Trailer", "X-Checksum, X-Stream-Complete")
	if _, err := w.Write([]byte(DEFAULT_STRING)); err != nil {
		return
	}
	if err := http.NewResponseController(w).Flush(); err != nil {
		return
	}
	w.Header().Set("X-Checksum", checksum([]byte(DEFAULT_STRING)))
	w.Header().Set("X-Stream-Complete", "true")
}
