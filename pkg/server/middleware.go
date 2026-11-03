package server

import (
	"bufio"
	"context"
	"log"
	"net"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"time"

	"github.com/TrueFix/httptoy/pkg/metrics"
	"github.com/gorilla/mux"
)

// responseLog preserves streaming, hijacking and ResponseController support.
type responseLog struct {
	http.ResponseWriter
	status            int
	written, hijacked bool
}

func (w *responseLog) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *responseLog) WriteHeader(status int) {
	if w.written {
		return
	}
	if status >= 200 || status == http.StatusSwitchingProtocols {
		w.status, w.written = status, true
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *responseLog) Write(data []byte) (int, error) {
	if !w.written {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}
func (w *responseLog) Flush() {
	_ = w.FlushError()
}
func (w *responseLog) FlushError() error {
	if !w.written {
		w.WriteHeader(http.StatusOK)
	}
	return http.NewResponseController(w.ResponseWriter).Flush()
}
func (w *responseLog) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, buffer, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil {
		w.hijacked = true
		w.status = http.StatusSwitchingProtocols
	}
	return conn, buffer, err
}

func requestMiddleware(next http.Handler) http.Handler {
	return requestMiddlewareWithMetrics(next, nil)
}

type observationKey struct{}
type requestObservation struct{ template string }

// Gorilla supplies the matched route on its derived request context. Share only
// this trusted template with the outer observer, never mux variables or a URL.
func observeRoute(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if observation, ok := req.Context().Value(observationKey{}).(*requestObservation); ok {
			if route := mux.CurrentRoute(req); route != nil {
				if template, err := route.GetPathTemplate(); err == nil {
					observation.template = template
				}
			}
		}
		next.ServeHTTP(w, req)
	})
}

func requestMiddlewareWithMetrics(next http.Handler, meter *metrics.Registry) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		started := time.Now()
		logged := &responseLog{ResponseWriter: w, status: http.StatusOK}
		observation := &requestObservation{template: "unmatched"}
		aborted, panicked := false, false
		if meter != nil {
			meter.Begin()
			req = req.WithContext(context.WithValue(req.Context(), observationKey{}, observation))
		}
		defer func() {
			duration := time.Since(started)
			if meter != nil {
				meter.Complete(req.Method, observation.template, logged.status, duration, aborted || req.Context().Err() != nil, panicked)
			}
			log.Printf("%s %s status=%d duration=%s", req.Method, safeLogPath(req), logged.status, duration)
		}()
		defer func() {
			if recovered := recover(); recovered != nil {
				if recovered == http.ErrAbortHandler {
					aborted = true
					panic(recovered)
				}
				panicked = true
				log.Printf("handler panic: %v\n%s", recovered, debug.Stack())
				if logged.written || logged.hijacked {
					aborted = true
					panic(http.ErrAbortHandler)
				}
				logged.WriteHeader(http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(logged, req)
	})
}

// Query strings and arbitrary echo-path values can contain credentials or
// captured payloads. Mask sensitive path fields and omit all query strings.
func safeLogPath(req *http.Request) string {
	path := req.URL.Path
	for _, prefix := range []string{"/basic/", "/body/", "/base64en/", "/base64de/", "/cookies/set/", "/cookies/delete/", "/anything/"} {
		if strings.HasPrefix(path, prefix) {
			return prefix + "[REDACTED]"
		}
	}
	if strings.HasPrefix(path, "/sessions/") {
		parts := strings.Split(path, "/")
		if len(parts) >= 3 {
			parts[2] = "{id}"
			return (&url.URL{Path: strings.Join(parts, "/")}).EscapedPath()
		}
	}
	return req.URL.EscapedPath()
}
