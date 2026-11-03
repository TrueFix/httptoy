package routes

import (
	"net/http"
	"strings"

	"github.com/gorilla/mux"
)

const (
	DEFAULT_STRING  = "i love httptoy"
	DEFAULT_UNICODE = "i ❤️ httptoy 🎉"
	INVALID_DATA    = "Invalid Data"
)

const (
	CONTENT_TEXT  = "text/plain; charset=utf-8"
	CONTENT_JSON  = "application/json; charset=utf-8"
	CONTENT_XML   = "application/xml; charset=utf-8"
	CONTENT_YAML  = "text/yaml; charset=utf-8"
	CONTENT_HTML  = "text/html; charset=utf-8"
	CONTENT_BYTES = "application/octet-stream"

	CONTENT_IMAGE_PNG  = "image/png"
	CONTENT_IMAGE_JPEG = "image/jpeg"
	CONTENT_IMAGE_WEBP = "image/webp"
	CONTENT_IMAGE_SVG  = "image/svg+xml"
)

// NewRouter preserves arbitrary testing paths and the existing 404 behavior for
// unsupported methods. Routes explicitly opt into each supported HTTP method.
func NewRouter() *mux.Router {
	r := mux.NewRouter().SkipClean(true)
	r.MethodNotAllowedHandler = http.NotFoundHandler()
	// Preserve redirects for a superfluous trailing slash, without cleaning
	// arbitrary paths under /anything or turning unsupported methods into 405s.
	r.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/" && strings.HasSuffix(req.URL.EscapedPath(), "/") {
			candidate := req.Clone(req.Context())
			candidate.URL.Path = strings.TrimSuffix(candidate.URL.Path, "/")
			candidate.URL.RawPath = strings.TrimSuffix(candidate.URL.RawPath, "/")
			match := &mux.RouteMatch{}
			if r.Match(candidate, match) && match.Route != nil && match.MatchErr == nil {
				status := http.StatusTemporaryRedirect
				if req.Method == http.MethodGet {
					status = http.StatusMovedPermanently
				}
				http.Redirect(w, req, candidate.URL.RequestURI(), status)
				return
			}
		}
		http.NotFound(w, req)
	})
	return r
}

func AddRoutes(r *mux.Router) {
	addAllMethods(r)
	addAllAuth(r)
	addAllStatus(r)
	addAllRequestInspection(r)
	addAllCompression(r)
	addAllResponse(r)
	addAllRequestRedirect(r)
	addAllImages(r)
	addAllCookies(r)
	addExtendedRoutes(r)
	addResilienceRoutes(r)
	addSessionRoutes(r)
	addProtocolRoutes(r)
	addDocumentationRoutes(r)
}
