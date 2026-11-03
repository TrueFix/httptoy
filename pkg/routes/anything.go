package routes

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/TrueFix/httptoy/pkg/httpx"
	"github.com/TrueFix/httptoy/pkg/parsers"
)

func anything(w http.ResponseWriter, req *http.Request) {
	if !applyCORS(w, req) {
		return
	}
	if req.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	req.Body = http.MaxBytesReader(w, req.Body, maxBodyBytes)
	body, err := io.ReadAll(req.Body)
	if err != nil {
		bodyError(w, err)
		return
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	data, text, err := parsers.ParseBodyDataWithError(req, contentType(req))
	if err != nil {
		bodyError(w, err)
		return
	}
	summary := requestSummary(req)
	httpx.JSON(w, http.StatusOK, map[string]any{
		"method": req.Method, "path": req.URL.EscapedPath(),
		"args": summary.Args, "headers": summary.Headers, "origin": summary.Origin,
		"url": summary.Url, "data": data, "text": text,
		"body_base64": base64.StdEncoding.EncodeToString(body),
		"body_type":   contentType(req), "body_length": req.ContentLength,
	})
}

func bodyError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	var limit *http.MaxBytesError
	if errors.As(err, &limit) {
		status = http.StatusRequestEntityTooLarge
	}
	httpx.JSON(w, status, map[string]any{"error": http.StatusText(status)})
}

// applyCORS is intentionally configurable for HTTP-client testing, not a global
// policy for the service. All reflected values are validated before use.
func applyCORS(w http.ResponseWriter, req *http.Request) bool {
	origin := defaultQuery(req, "allow_origin", "*")
	credentials, err := strconv.ParseBool(defaultQuery(req, "allow_credentials", "false"))
	if err != nil || !validOrigin(origin) || credentials && origin == "*" {
		invalidRequest(w, "invalid allow_origin or allow_credentials; credentials require an explicit origin")
		return false
	}
	methods := defaultQuery(req, "allow_methods", "GET,POST,PUT,PATCH,DELETE,HEAD,OPTIONS")
	for _, method := range strings.Split(methods, ",") {
		if !validToken(strings.TrimSpace(method)) {
			invalidRequest(w, "invalid allow_methods")
			return false
		}
	}
	headers := defaultQuery(req, "allow_headers", req.Header.Get("Access-Control-Request-Headers"))
	if headers != "" {
		for _, header := range strings.Split(headers, ",") {
			if !validToken(strings.TrimSpace(header)) {
				invalidRequest(w, "invalid allow_headers")
				return false
			}
		}
	}
	age, err := boundedNumber(defaultQuery(req, "max_age", "600"), 600, 0, 86400)
	if err != nil {
		invalidRequest(w, err.Error())
		return false
	}
	if len(methods)+len(headers)+len(origin) > 8192 {
		invalidRequest(w, "CORS configuration too large")
		return false
	}
	w.Header().Set("Vary", "Origin, Access-Control-Request-Method, Access-Control-Request-Headers")
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Methods", methods)
	if headers != "" {
		w.Header().Set("Access-Control-Allow-Headers", headers)
	}
	w.Header().Set("Access-Control-Max-Age", strconv.Itoa(age))
	if credentials {
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	}
	return true
}

func validOrigin(origin string) bool {
	if origin == "*" || origin == "null" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == "" && validHeaderValue(origin)
}
