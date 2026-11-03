package routes

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"

	"github.com/TrueFix/httptoy/pkg/httpx"
	"github.com/gorilla/mux"
)

var routeVariable = regexp.MustCompile(`\{([A-Za-z0-9_]+)(?::[^}]+)?\}`)

type routeDescription struct {
	path    string
	methods []string
}

func routeInventory(router *mux.Router) ([]routeDescription, error) {
	var inventory []routeDescription
	err := router.Walk(func(route *mux.Route, _ *mux.Router, _ []*mux.Route) error {
		path, err := route.GetPathTemplate()
		if err != nil {
			return err
		}
		methods, err := route.GetMethods()
		if err != nil {
			return err
		}
		inventory = append(inventory, routeDescription{path: path, methods: methods})
		return nil
	})
	return inventory, err
}

func addDocumentationRoutes(r *mux.Router) {
	// Build lazily: Server registers its homepage, version and favicon after the
	// API routes. All router mutation finishes before the first request is served.
	var once sync.Once
	var openAPI, collection []byte
	var buildErr error
	build := func() {
		inventory, err := routeInventory(r)
		if err != nil {
			buildErr = err
			return
		}
		openAPI, err = json.Marshal(openAPIDocument(inventory))
		if err != nil {
			buildErr = err
			return
		}
		collection, buildErr = json.Marshal(postmanCollection(inventory))
	}
	r.HandleFunc("/openapi.json", func(w http.ResponseWriter, req *http.Request) {
		once.Do(build)
		if buildErr != nil {
			w.WriteHeader(500)
			return
		}
		httpx.Data(w, 200, CONTENT_JSON, openAPI)
	}).Methods("GET")
	r.HandleFunc("/collection.json", func(w http.ResponseWriter, req *http.Request) {
		once.Do(build)
		if buildErr != nil {
			w.WriteHeader(500)
			return
		}
		w.Header().Set("Content-Disposition", `attachment; filename="httptoy.postman_collection.json"`)
		httpx.Data(w, 200, CONTENT_JSON, collection)
	}).Methods("GET")
	r.HandleFunc("/capabilities", capabilities).Methods("GET")
}

func capabilities(w http.ResponseWriter, req *http.Request) {
	httpx.JSON(w, 200, map[string]any{
		"router": "gorilla/mux", "server": "net/http",
		"protocols":       map[string]any{"http_1_1": true, "http_2": "requires an HTTP/2-enabled TLS deployment; default listener is plaintext HTTP/1.1", "websocket": true, "sse": true, "request_trailers": true},
		"content_codings": []string{"br", "gzip", "deflate", "identity"},
		"charsets":        []string{"utf-8", "utf-16", "utf-16le", "utf-16be", "iso-8859-1"},
		"checksums":       []string{"sha256", "sha512"},
		"limits":          map[string]any{"request_body_bytes": maxBodyBytes, "compressed_body_bytes": maxBodyBytes, "expanded_body_bytes": maxBodyBytes, "concurrent_validation_decompression": cap(protocolWorkSlots), "delay_seconds": maxDelaySeconds, "concurrent_delays": cap(delaySlots), "concurrent_streams": cap(streamSlots), "websocket_connections": cap(websocketSlots), "capture_body_bytes": maxCaptureBytes, "sessions": maxSessions, "session_capture_bytes": maxSessionBytes, "session_requests": maxSessionRequests, "session_idempotency_keys": maxIdempotencyKeys, "session_ttl_seconds": 600},
		"sessions":        map[string]any{"storage": "process-local, ephemeral", "authentication": "Authorization: Bearer <session token>", "capture_sensitive_headers": "redacted; bodies and query strings may still contain secrets", "outbound_forwarding": false},
		"discovery":       map[string]string{"openapi": "/openapi.json", "postman": "/collection.json", "metrics": "/metrics"},
	})
}

func openAPIDocument(inventory []routeDescription) map[string]any {
	paths := make(map[string]any)
	for _, route := range inventory {
		path := routeVariable.ReplaceAllString(route.path, "{$1}")
		item, exists := paths[path].(map[string]any)
		if !exists {
			item = make(map[string]any)
			paths[path] = item
		}
		parameters := make([]any, 0)
		for _, variable := range routeVariable.FindAllStringSubmatch(route.path, -1) {
			schema := map[string]any{"type": "string"}
			switch variable[1] {
			case "count", "size", "seconds", "times", "delay", "code", "status":
				schema = map[string]any{"type": "integer"}
			}
			parameters = append(parameters, map[string]any{"name": variable[1], "in": "path", "required": true, "schema": schema})
		}
		for _, method := range route.methods {
			operation := map[string]any{"summary": endpointSummary(path), "operationId": operationID(method, path), "parameters": parameters, "responses": map[string]any{"default": map[string]any{"description": "Endpoint-specific HTTP response; test fixtures can intentionally return errors, streams, redirects or incomplete bodies. See README for precise options and limits."}}}
			if strings.HasPrefix(path, "/sessions/{id}") {
				operation["security"] = []any{map[string]any{"SessionBearer": []string{}}}
			}
			if path == "/basic" || strings.HasPrefix(path, "/basic/") {
				operation["security"] = []any{map[string]any{"TestBasic": []string{}}}
			}
			if path == "/bearer" {
				operation["security"] = []any{map[string]any{"TestBearer": []string{}}}
			}
			if path == "/auth/digest" {
				operation["security"] = []any{map[string]any{"TestDigest": []string{}}}
			}
			if method == "POST" || method == "PUT" || method == "PATCH" {
				operation["requestBody"] = map[string]any{"required": false, "content": map[string]any{"application/octet-stream": map[string]any{"schema": map[string]any{"type": "string", "format": "binary"}}, "application/json": map[string]any{"schema": map[string]any{}}}}
			}
			if path == "/sessions" && method == "POST" {
				operation["requestBody"] = map[string]any{"required": false, "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/SessionConfiguration"}}}}
				operation["responses"] = map[string]any{"201": map[string]any{"description": "Created: save the id and token; the bearer token is returned only here."}, "400": map[string]any{"description": "Invalid configuration"}, "503": map[string]any{"description": "Session capacity exhausted"}}
			}
			if path == "/sessions/{id}/idempotency" {
				operation["parameters"] = append(append([]any{}, parameters...), map[string]any{"name": "Idempotency-Key", "in": "header", "required": true, "schema": map[string]any{"type": "string", "minLength": 1, "maxLength": 128}})
			}
			if path == "/upload" {
				operation["requestBody"] = map[string]any{"required": true, "content": map[string]any{"multipart/form-data": map[string]any{"schema": map[string]any{"type": "object", "properties": map[string]any{"file": map[string]any{"type": "string", "format": "binary"}, "field": map[string]any{"type": "string"}}}}}}
			}
			if path == "/negotiate" || path == "/language" || path == "/compression/negotiate" || path == "/sse/resume" || path == "/expect-continue" {
				header := map[string]string{"/negotiate": "Accept", "/language": "Accept-Language", "/compression/negotiate": "Accept-Encoding", "/sse/resume": "Last-Event-ID", "/expect-continue": "Expect"}[path]
				operation["parameters"] = append(append([]any{}, parameters...), map[string]any{"name": header, "in": "header", "required": false, "schema": map[string]string{"type": "string"}})
			}
			key := strings.ToLower(method)
			if method == "CONNECT" {
				key = "x-connect"
			} // CONNECT is not an OpenAPI Path Item operation.
			item[key] = operation
		}
	}
	return map[string]any{"openapi": "3.1.0", "info": map[string]any{"title": "httptoy", "version": "1.0.0", "description": "Bounded HTTP protocol fixtures. Session routes require their own bearer token. Never submit production credentials. Catch-all path parameters may span multiple segments. CONNECT is documented as x-connect because OpenAPI has no CONNECT operation."}, "servers": []any{map[string]string{"url": "/"}}, "paths": paths, "components": map[string]any{"securitySchemes": map[string]any{"SessionBearer": map[string]string{"type": "http", "scheme": "bearer", "bearerFormat": "opaque session token"}, "TestBasic": map[string]string{"type": "http", "scheme": "basic"}, "TestBearer": map[string]string{"type": "http", "scheme": "bearer"}, "TestDigest": map[string]string{"type": "http", "scheme": "digest"}}, "schemas": map[string]any{"SessionConfiguration": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
		"ttl_seconds":    map[string]any{"type": "integer", "minimum": 1, "maximum": 600, "default": 300},
		"sequence":       map[string]any{"type": "array", "minItems": 1, "maxItems": 32, "items": map[string]any{"type": "integer", "enum": finalStatusCodes()}, "default": []int{503, 503, 200}},
		"rate_limit":     map[string]any{"type": "integer", "minimum": 1, "maximum": 1000, "default": 5},
		"window_seconds": map[string]any{"type": "integer", "minimum": 1, "maximum": 300, "default": 60},
		"poll_after":     map[string]any{"type": "integer", "minimum": 0, "maximum": 100, "default": 2},
	}}}}}
}

func operationID(method, path string) string {
	return strings.ToLower(method) + "_" + strings.NewReplacer("/", "_", "{", "", "}", "", "-", "_", ".", "_").Replace(path)
}

func endpointSummary(path string) string {
	summaries := map[string]string{
		"/metrics":                   "Scrape aggregate Prometheus metrics with bounded route-template labels",
		"/sessions":                  "Create an isolated ephemeral workflow sandbox",
		"/sessions/{id}":             "Delete an authenticated sandbox",
		"/sessions/{id}/capture":     "Capture a bounded webhook without forwarding it",
		"/sessions/{id}/requests":    "Inspect captured requests in order",
		"/sessions/{id}/sequence":    "Consume a configured status sequence, then repeat the final status",
		"/sessions/{id}/idempotency": "Replay a result or reject a conflicting idempotency key",
		"/sessions/{id}/rate-limit":  "Exercise a per-session fixed-window rate limit",
		"/sessions/{id}/poll":        "Simulate a job progressing from pending to complete",
		"/negotiate":                 "Negotiate a representation using Accept",
		"/language":                  "Negotiate a greeting using Accept-Language",
		"/problem/{status}":          "Return an RFC 9457 problem response",
		"/sse/resume":                "Resume finite SSE events using Last-Event-ID",
		"/expect-continue":           "Accept or reject an upload before reading its body",
		"/request-trailers":          "Read a bounded request and echo its trailers",
		"/decompress/{coding}":       "Inspect a bounded compressed upload",
		"/validate/{format}":         "Validate JSON, XML or multipart input",
		"/openapi.json":              "Discover all registered routes as OpenAPI 3.1",
		"/capabilities":              "Discover supported features and resource limits",
		"/collection.json":           "Download a Postman collection with session variables",
	}
	if summary, ok := summaries[path]; ok {
		return summary
	}
	return "HTTP test fixture: " + path
}

func postmanCollection(inventory []routeDescription) map[string]any {
	items := make([]any, 0)
	for _, route := range inventory {
		path := routeVariable.ReplaceAllStringFunc(route.path, func(variable string) string {
			name := routeVariable.FindStringSubmatch(variable)[1]
			examples := map[string]string{"count": "3", "size": "26", "seconds": "1", "times": "2", "delay": "0", "code": "200", "status": "400", "username": "user", "password": "pass", "key": "demo", "value": "demo", "data": "hello", "charset": "utf-8", "algorithm": "sha256", "filename": "example.txt", "coding": "gzip", "format": "json", "header_name": "User-Agent", "id": "{{session_id}}", "path": "example"}
			if value, ok := examples[name]; ok {
				return value
			}
			return "example"
		})
		if path == "/informational/200" {
			path = "/informational/102"
		}
		if strings.HasPrefix(route.path, "/base64de/") {
			path = "/base64de/aGVsbG8="
		}
		for _, method := range route.methods {
			request := map[string]any{"method": method, "url": "{{base_url}}" + path, "description": endpointSummary(routeVariable.ReplaceAllString(route.path, "{$1}"))}
			if strings.HasPrefix(route.path, "/sessions/{id}") {
				request["auth"] = map[string]any{"type": "bearer", "bearer": []any{map[string]string{"key": "token", "value": "{{session_token}}", "type": "string"}}}
			}
			if route.path == "/basic" || strings.HasPrefix(route.path, "/basic/") {
				request["auth"] = map[string]any{"type": "basic", "basic": []any{map[string]string{"key": "username", "value": "user", "type": "string"}, map[string]string{"key": "password", "value": "pass", "type": "string"}}}
			}
			if route.path == "/bearer" {
				request["auth"] = map[string]any{"type": "bearer", "bearer": []any{map[string]string{"key": "token", "value": "example-token", "type": "string"}}}
			}
			if route.path == "/auth/digest" {
				request["auth"] = map[string]any{"type": "digest", "digest": []any{map[string]string{"key": "username", "value": "user", "type": "string"}, map[string]string{"key": "password", "value": "passwd", "type": "string"}}}
			}
			if route.path == "/redirect-to" {
				request["url"] = "{{base_url}}/redirect-to?url=/get"
			}
			if route.path == "/cookies/set" {
				request["url"] = "{{base_url}}/cookies/set?demo=value"
			}
			if method == "POST" || method == "PUT" || method == "PATCH" {
				request["header"] = []any{map[string]string{"key": "Content-Type", "value": "application/json"}}
				request["body"] = map[string]string{"mode": "raw", "raw": "{}"}
			}
			if strings.HasSuffix(route.path, "/idempotency") {
				request["header"] = []any{map[string]string{"key": "Idempotency-Key", "value": "example-key"}, map[string]string{"key": "Content-Type", "value": "application/json"}}
			}
			if route.path == "/upload" || route.path == "/validate/{format}" && strings.HasSuffix(path, "/multipart") {
				request["header"] = []any{}
				request["body"] = map[string]any{"mode": "formdata", "formdata": []any{map[string]string{"key": "field", "value": "demo", "type": "text"}}}
			}
			item := map[string]any{"name": fmt.Sprintf("%s %s", method, route.path), "request": request}
			if route.path == "/sessions" && method == "POST" {
				item["event"] = []any{map[string]any{"listen": "test", "script": map[string]any{"type": "text/javascript", "exec": []string{"if (pm.response.code === 201) { const session = pm.response.json(); pm.collectionVariables.set('session_id', session.id); pm.collectionVariables.set('session_token', session.token); }"}}}}
			}
			items = append(items, item)
		}
	}
	return map[string]any{"info": map[string]string{"name": "httptoy", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json", "description": "Run POST /sessions to populate session_id/session_token. Fault endpoints intentionally fail. Some protocol and upload examples require custom headers/body bytes; see README."}, "variable": []any{map[string]string{"key": "base_url", "value": "http://localhost:8000"}, map[string]string{"key": "session_id", "value": ""}, map[string]string{"key": "session_token", "value": ""}}, "item": items}
}
