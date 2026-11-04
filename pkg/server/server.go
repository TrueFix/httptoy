package server

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/TrueFix/httptoy/pkg/httpx"
	"github.com/TrueFix/httptoy/pkg/routes"
	samplefiles "github.com/TrueFix/httptoy/sample_files"
)

// Server builds the Gorilla router and standard HTTP middleware.
func Server() http.Handler {
	r := routes.NewRouter()
	routes.AddRoutes(r)
	registerWebsite(r)
	r.HandleFunc("/version", version).Methods(http.MethodGet)
	favicon, err := samplefiles.Files.ReadFile("images/favicon.ico")
	if err != nil {
		panic(err)
	}
	r.HandleFunc("/favicon.ico", func(w http.ResponseWriter, req *http.Request) {
		httpx.Data(w, http.StatusOK, "image/x-icon", favicon)
	}).Methods(http.MethodGet, http.MethodHead)
	var proxies []string
	if value := strings.TrimSpace(os.Getenv("TRUSTED_PROXIES")); value != "" {
		proxies = strings.Split(value, ",")
	}
	proxyMiddleware, err := httpx.TrustedProxies(proxies)
	if err != nil {
		panic(err)
	}
	meter := instrumentRouter(r)
	return requestMiddlewareWithMetrics(proxyMiddleware(websiteHeaders(r)), meter)
}

// HTTPServer sets deadlines suitable for the service's maximum ten-second delay.
func HTTPServer(address string) *http.Server {
	return &http.Server{
		Addr: address, Handler: Server(), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 15 * time.Second, WriteTimeout: 20 * time.Second,
		IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20,
	}
}

func version(w http.ResponseWriter, req *http.Request) {
	version, commit := buildInfo()
	text := fmt.Sprintf("VERSION=%s\nGIT_SHA=%s", version, commit)
	httpx.Data(w, http.StatusOK, routes.CONTENT_TEXT, []byte(text))
}
