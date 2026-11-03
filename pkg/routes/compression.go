package routes

import (
	"net/http"

	"github.com/TrueFix/httptoy/pkg/encoding"
	"github.com/TrueFix/httptoy/pkg/httpx"
	"github.com/gorilla/mux"
)

// Immutable payloads are computed once, before requests can run.
var (
	deflatePayload = encoding.CompressZlib([]byte(DEFAULT_STRING))
	brotliPayload  = encoding.CompressBrotli([]byte(DEFAULT_STRING))
	gzipPayload    = encoding.CompressGzip([]byte(DEFAULT_STRING))
	unicodePayload = []byte(DEFAULT_UNICODE)
)

func addAllCompression(r *mux.Router) {
	for _, route := range []struct {
		path, coding string
		data         []byte
	}{
		{"/deflate", "deflate", deflatePayload}, {"/zlib", "deflate", deflatePayload},
		{"/brotli", "br", brotliPayload}, {"/gzip", "gzip", gzipPayload}, {"/utf8", "", unicodePayload},
	} {
		r.HandleFunc(route.path, func(w http.ResponseWriter, req *http.Request) {
			if route.coding != "" {
				w.Header().Set("Content-Encoding", route.coding)
			}
			httpx.Data(w, http.StatusOK, CONTENT_TEXT, route.data)
		}).Methods("GET")
	}
}
