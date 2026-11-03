package routes

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

var fixtureModTime = time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)

func cacheResponse(w http.ResponseWriter, req *http.Request) {
	seconds, err := boundedNumber(mux.Vars(req)["seconds"], 0, 0, 86400)
	if err != nil {
		invalidRequest(w, err.Error())
		return
	}
	w.Header().Set("Cache-Control", "public, max-age="+strconv.Itoa(seconds))
	w.Header().Set("ETag", fmt.Sprintf("\"%x\"", sha256.Sum256([]byte(DEFAULT_STRING))))
	w.Header().Set("Content-Type", CONTENT_TEXT)
	http.ServeContent(w, req, "cache.txt", fixtureModTime, strings.NewReader(DEFAULT_STRING))
}

func etagResponse(w http.ResponseWriter, req *http.Request) {
	value := mux.Vars(req)["value"]
	if len(value) > 128 {
		invalidRequest(w, "ETag value exceeds 128 bytes")
		return
	}
	for _, ch := range value {
		if ch < 0x21 || ch > 0x7e || ch == '"' {
			invalidRequest(w, "ETag must contain printable ASCII except quotes and spaces")
			return
		}
	}
	w.Header().Set("ETag", "\""+value+"\"")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", CONTENT_TEXT)
	http.ServeContent(w, req, "etag.txt", time.Time{}, strings.NewReader(value))
}

func rangeResponse(w http.ResponseWriter, req *http.Request) {
	size, err := boundedNumber(mux.Vars(req)["size"], 0, 0, maxBodyBytes)
	if err != nil {
		invalidRequest(w, err.Error())
		return
	}
	data := patternBytes(size)
	w.Header().Set("Content-Type", CONTENT_BYTES)
	w.Header().Set("ETag", "\"range-"+strconv.Itoa(size)+"\"")
	http.ServeContent(w, req, "range.bin", fixtureModTime, bytes.NewReader(data))
}
