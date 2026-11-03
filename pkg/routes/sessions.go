package routes

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/TrueFix/httptoy/pkg/httpx"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

const (
	maxSessions        = 64
	maxSessionBytes    = 256 << 10
	maxCaptureBytes    = 64 << 10
	maxSessionRequests = 32
	maxIdempotencyKeys = 32
)

type sessionConfig struct {
	TTLSeconds    int   `json:"ttl_seconds"`
	Sequence      []int `json:"sequence"`
	RateLimit     int   `json:"rate_limit"`
	WindowSeconds int   `json:"window_seconds"`
	PollAfter     int   `json:"poll_after"`
}

type capturedRequest struct {
	ID      int                 `json:"id"`
	Method  string              `json:"method"`
	URL     string              `json:"url"`
	Headers map[string][]string `json:"headers"`
	Body    string              `json:"body_base64"`
	At      time.Time           `json:"captured_at"`
}

type idempotencyEntry struct {
	fingerprint [32]byte
	response    map[string]any
}

type workflowSession struct {
	tokenHash     [32]byte
	expires       time.Time
	config        sessionConfig
	requests      []capturedRequest
	storedBytes   int
	sequenceIndex int
	rateUsed      int
	rateReset     time.Time
	pollAttempts  int
	idempotency   map[string]idempotencyEntry
}

type sessionStore struct {
	sync.Mutex
	entries map[string]*workflowSession
	now     func() time.Time
}

func newSessionStore() *sessionStore {
	return &sessionStore{entries: make(map[string]*workflowSession), now: time.Now}
}

// One process-wide store/cleanup worker, not one goroutine per router or session.
var workflowStore = func() *sessionStore {
	store := newSessionStore()
	go func() {
		for range time.Tick(time.Second) {
			store.Lock()
			store.prune()
			store.Unlock()
		}
	}()
	return store
}()

func (s *sessionStore) prune() {
	now := s.now()
	for id, session := range s.entries {
		if !now.Before(session.expires) {
			delete(s.entries, id)
		}
	}
}

func addSessionRoutes(r *mux.Router) { registerSessionRoutes(r, workflowStore) }

func registerSessionRoutes(r *mux.Router, store *sessionStore) {
	r.HandleFunc("/sessions", store.create).Methods("POST")
	r.HandleFunc("/sessions/{id}", store.remove).Methods("DELETE")
	r.HandleFunc("/sessions/{id}/capture", store.capture).Methods("POST")
	r.HandleFunc("/sessions/{id}/requests", store.requests).Methods("GET")
	r.HandleFunc("/sessions/{id}/sequence", store.sequence).Methods("GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "CONNECT", "TRACE")
	r.HandleFunc("/sessions/{id}/idempotency", store.idempotency).Methods("POST")
	r.HandleFunc("/sessions/{id}/rate-limit", store.rateLimit).Methods("GET")
	r.HandleFunc("/sessions/{id}/poll", store.poll).Methods("GET")
}

type sessionResult struct {
	status  int
	body    any
	headers map[string]string
}

// use authorizes and performs bounded in-memory work under the lock. It never
// reads a client body or writes a network response while holding that lock.
func (s *sessionStore) use(w http.ResponseWriter, req *http.Request, action func(*workflowSession) sessionResult) bool {
	w.Header().Set("Cache-Control", "no-store")
	scheme, token, ok := strings.Cut(req.Header.Get("Authorization"), " ")
	formatted := ok && strings.EqualFold(scheme, "Bearer") && len(token) == 43
	var hash [32]byte
	if formatted {
		hash = sha256.Sum256([]byte(token))
	}
	s.Lock()
	s.prune()
	session := s.entries[mux.Vars(req)["id"]]
	valid := formatted && session != nil
	if valid {
		valid = subtle.ConstantTimeCompare(hash[:], session.tokenHash[:]) == 1
	}
	if !valid {
		s.Unlock()
		w.Header().Set("WWW-Authenticate", "Bearer")
		httpx.JSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid or expired session credentials"})
		return false
	}
	result := func() sessionResult { defer s.Unlock(); return action(session) }()
	if result.status == 0 {
		return true
	}
	for name, value := range result.headers {
		w.Header().Set(name, value)
	}
	if result.body == nil {
		w.WriteHeader(result.status)
	} else {
		httpx.JSON(w, result.status, result.body)
	}
	return true
}

func randomSessionSecret(bytes int) string {
	buffer := make([]byte, bytes)
	if _, err := rand.Read(buffer); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer)
}

func (s *sessionStore) create(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	req.Body = http.MaxBytesReader(w, req.Body, 8192)
	config := sessionConfig{TTLSeconds: 300, Sequence: []int{503, 503, 200}, RateLimit: 5, WindowSeconds: 60, PollAfter: 2}
	decoder := json.NewDecoder(req.Body)
	decoder.DisallowUnknownFields()
	input := &config
	if err := decoder.Decode(&input); err != nil && err != io.EOF {
		bodyError(w, err)
		return
	}
	if input == nil {
		invalidRequest(w, "session configuration must be an object")
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			bodyError(w, err)
		} else {
			invalidRequest(w, "expected one JSON configuration object")
		}
		return
	}
	if config.TTLSeconds < 1 || config.TTLSeconds > 600 || len(config.Sequence) < 1 || len(config.Sequence) > 32 || config.RateLimit < 1 || config.RateLimit > 1000 || config.WindowSeconds < 1 || config.WindowSeconds > 300 || config.PollAfter < 0 || config.PollAfter > 100 {
		invalidRequest(w, "invalid session configuration limits")
		return
	}
	for _, code := range config.Sequence {
		if !validFinalStatus(code) {
			invalidRequest(w, "sequence requires recognized 200–599 status codes")
			return
		}
	}
	id, token := randomSessionSecret(16), randomSessionSecret(32)
	s.Lock()
	s.prune()
	if len(s.entries) >= maxSessions {
		sessionRejections.Add(1)
		s.Unlock()
		w.Header().Set("Retry-After", "1")
		httpx.JSON(w, 503, map[string]any{"error": "session capacity exhausted"})
		return
	}
	now := s.now()
	expires := now.Add(time.Duration(config.TTLSeconds) * time.Second)
	s.entries[id] = &workflowSession{tokenHash: sha256.Sum256([]byte(token)), expires: expires, config: config, requests: make([]capturedRequest, 0), rateReset: now.Add(time.Duration(config.WindowSeconds) * time.Second), idempotency: make(map[string]idempotencyEntry)}
	s.Unlock()
	w.Header().Set("Location", "/sessions/"+id)
	httpx.JSON(w, http.StatusCreated, map[string]any{"id": id, "token": token, "expires_at": expires, "config": config})
}

func (s *sessionStore) remove(w http.ResponseWriter, req *http.Request) {
	s.use(w, req, func(session *workflowSession) sessionResult {
		delete(s.entries, mux.Vars(req)["id"])
		return sessionResult{status: 204}
	})
}

func sensitiveCaptureHeader(name string) bool {
	name = strings.ToLower(name)
	return name == "authorization" || name == "proxy-authorization" || name == "cookie" || name == "set-cookie" || strings.Contains(name, "token") || strings.Contains(name, "secret") || strings.Contains(name, "key")
}

func (s *sessionStore) capture(w http.ResponseWriter, req *http.Request) {
	if !s.use(w, req, func(*workflowSession) sessionResult { return sessionResult{} }) {
		return
	}
	if len(req.URL.RequestURI()) > 4096 || len(req.Header) > 32 {
		invalidRequest(w, "capture URL/header limits exceeded")
		return
	}
	headers := make(map[string][]string, len(req.Header))
	headerBytes, headerValues := 0, 0
	for name, values := range req.Header {
		for _, value := range values {
			headerBytes += len(name) + len(value)
			headerValues++
		}
		if headerBytes > 16384 || headerValues > 64 {
			invalidRequest(w, "capture headers exceed 16 KiB or 64 values")
			return
		}
		if sensitiveCaptureHeader(name) {
			headers[name] = []string{"[REDACTED]"}
		} else {
			headers[name] = append([]string(nil), values...)
		}
	}
	req.Body = http.MaxBytesReader(w, req.Body, maxCaptureBytes)
	body, err := io.ReadAll(req.Body)
	if err != nil {
		bodyError(w, err)
		return
	}
	encoded := base64.StdEncoding.EncodeToString(body)
	cost := len(encoded) + headerBytes + len(req.URL.RequestURI()) + 1024
	s.use(w, req, func(session *workflowSession) sessionResult {
		if len(session.requests) >= maxSessionRequests || session.storedBytes+cost > maxSessionBytes {
			return sessionResult{status: 413, body: map[string]any{"error": "session capture storage exhausted"}}
		}
		id := len(session.requests) + 1
		session.requests = append(session.requests, capturedRequest{ID: id, Method: req.Method, URL: req.URL.RequestURI(), Headers: headers, Body: encoded, At: s.now()})
		session.storedBytes += cost
		return sessionResult{status: 202, body: map[string]any{"id": id, "bytes": len(body)}}
	})
}

func (s *sessionStore) requests(w http.ResponseWriter, req *http.Request) {
	s.use(w, req, func(session *workflowSession) sessionResult {
		// Records and their maps are immutable after append; copying the slice
		// lets JSON encoding happen outside the store lock without races.
		requests := append([]capturedRequest{}, session.requests...)
		return sessionResult{status: 200, body: map[string]any{"requests": requests, "count": len(requests), "stored_bytes": session.storedBytes}}
	})
}

func (s *sessionStore) sequence(w http.ResponseWriter, req *http.Request) {
	s.use(w, req, func(session *workflowSession) sessionResult {
		index := session.sequenceIndex
		code := session.config.Sequence[index]
		if index < len(session.config.Sequence)-1 {
			session.sequenceIndex++
		}
		result := sessionResult{status: code}
		if code != 204 && code != 205 && code != 304 {
			result.body = map[string]any{"status": code, "index": index}
		}
		return result
	})
}

func (s *sessionStore) idempotency(w http.ResponseWriter, req *http.Request) {
	if !s.use(w, req, func(*workflowSession) sessionResult { return sessionResult{} }) {
		return
	}
	key := req.Header.Get("Idempotency-Key")
	if len(key) < 1 || len(key) > 128 {
		invalidRequest(w, "Idempotency-Key must have 1–128 printable non-space ASCII bytes")
		return
	}
	for _, ch := range key {
		if ch < 33 || ch > 126 {
			invalidRequest(w, "invalid Idempotency-Key")
			return
		}
	}
	req.Body = http.MaxBytesReader(w, req.Body, maxBodyBytes)
	digest := sha256.New()
	digest.Write([]byte(req.Header.Get("Content-Type") + "\n"))
	size, err := io.Copy(digest, req.Body)
	if err != nil {
		bodyError(w, err)
		return
	}
	var fingerprint [32]byte
	copy(fingerprint[:], digest.Sum(nil))
	s.use(w, req, func(session *workflowSession) sessionResult {
		if entry, ok := session.idempotency[key]; ok {
			if entry.fingerprint != fingerprint {
				return sessionResult{status: 409, body: map[string]any{"error": "key was used with a different payload or Content-Type"}}
			}
			return sessionResult{status: 201, body: entry.response, headers: map[string]string{"Idempotency-Replayed": "true"}}
		}
		if len(session.idempotency) >= maxIdempotencyKeys {
			return sessionResult{status: 429, body: map[string]any{"error": "session idempotency capacity exhausted"}}
		}
		response := map[string]any{"request_id": uuid.NewString(), "fingerprint": hex.EncodeToString(fingerprint[:]), "bytes": size}
		session.idempotency[key] = idempotencyEntry{fingerprint: fingerprint, response: response}
		return sessionResult{status: 201, body: response, headers: map[string]string{"Idempotency-Replayed": "false"}}
	})
}

func (s *sessionStore) rateLimit(w http.ResponseWriter, req *http.Request) {
	s.use(w, req, func(session *workflowSession) sessionResult {
		now := s.now()
		if !now.Before(session.rateReset) {
			session.rateUsed = 0
			session.rateReset = now.Add(time.Duration(session.config.WindowSeconds) * time.Second)
		}
		status := 200
		if session.rateUsed >= session.config.RateLimit {
			status = 429
		} else {
			session.rateUsed++
		}
		remaining := session.config.RateLimit - session.rateUsed
		headers := map[string]string{"X-RateLimit-Limit": strconv.Itoa(session.config.RateLimit), "X-RateLimit-Remaining": strconv.Itoa(remaining), "X-RateLimit-Reset": strconv.FormatInt(session.rateReset.Unix(), 10)}
		if status == 429 {
			headers["Retry-After"] = strconv.Itoa(int((session.rateReset.Sub(now) + time.Second - 1) / time.Second))
		}
		return sessionResult{status: status, headers: headers, body: map[string]any{"allowed": status == 200, "remaining": remaining, "reset_at": session.rateReset}}
	})
}

func (s *sessionStore) poll(w http.ResponseWriter, req *http.Request) {
	s.use(w, req, func(session *workflowSession) sessionResult {
		if session.pollAttempts <= session.config.PollAfter {
			session.pollAttempts++
		}
		status, state := 200, "complete"
		headers := make(map[string]string)
		if session.pollAttempts <= session.config.PollAfter {
			status, state = 202, "pending"
			headers["Retry-After"] = "1"
		}
		return sessionResult{status: status, headers: headers, body: map[string]any{"status": state, "attempts": session.pollAttempts}}
	})
}
