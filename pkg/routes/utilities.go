package routes

import (
	"crypto/sha256"
	"encoding/hex"
	"html"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/TrueFix/httptoy/pkg/httpx"
	"github.com/gorilla/mux"
)

func responseHeaders(w http.ResponseWriter, req *http.Request) {
	values := req.URL.Query()
	if len(values) > 32 || len(req.URL.RawQuery) > 16384 {
		invalidRequest(w, "too many response headers")
		return
	}
	for name, entries := range values {
		if !validToken(name) {
			invalidRequest(w, "invalid header name")
			return
		}
		switch strings.ToLower(name) {
		case "connection", "content-length", "transfer-encoding", "trailer", "upgrade", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "content-encoding":
			invalidRequest(w, "framing, connection, and encoding headers cannot be overridden")
			return
		}
		for _, value := range entries {
			if len(value) > 4096 || !validHeaderValue(value) {
				invalidRequest(w, "invalid header value")
				return
			}
		}
	}
	for name, entries := range values {
		for _, value := range entries {
			w.Header().Add(name, value)
		}
	}
	httpx.JSON(w, http.StatusOK, values)
}

func redirectTo(w http.ResponseWriter, req *http.Request) {
	target := req.URL.Query().Get("url")
	u, err := url.Parse(target)
	if err != nil || target == "" || len(target) > 4096 || !validHeaderValue(target) || u.User != nil ||
		!((u.Scheme == "http" || u.Scheme == "https") && u.Host != "" || u.Scheme == "" && u.Host == "" && strings.HasPrefix(target, "/") && !strings.HasPrefix(target, "//")) {
		invalidRequest(w, "url must be an absolute HTTP(S) URL or a local absolute path")
		return
	}
	status, err := boundedNumber(req.URL.Query().Get("status_code"), 302, 300, 399)
	if err != nil || status != 301 && status != 302 && status != 303 && status != 307 && status != 308 {
		invalidRequest(w, "status_code must be 301, 302, 303, 307, or 308")
		return
	}
	// Return a Location only: never fetch or resolve the destination server-side.
	http.Redirect(w, req, target, status)
}

func links(w http.ResponseWriter, req *http.Request) {
	count, err := boundedNumber(mux.Vars(req)["count"], 0, 0, 100)
	if err != nil {
		invalidRequest(w, err.Error())
		return
	}
	var body strings.Builder
	body.WriteString("<!doctype html><html lang=\"en\"><head><title>httptoy links</title></head><body><nav>")
	for i := 0; i < count; i++ {
		label := strconv.Itoa(i)
		body.WriteString("<a href=\"/anything/links/" + label + "\">" + html.EscapeString(label) + "</a>\n")
	}
	body.WriteString("</nav></body></html>")
	httpx.Data(w, http.StatusOK, CONTENT_HTML, []byte(body.String()))
}

type uploadedFile struct {
	Field       string `json:"field"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
	SHA256      string `json:"sha256"`
}

func checksum(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func upload(w http.ResponseWriter, req *http.Request) {
	req.Body = http.MaxBytesReader(w, req.Body, maxBodyBytes)
	reader, err := req.MultipartReader()
	if err != nil {
		bodyError(w, err)
		return
	}
	fields := make(map[string][]string)
	files := make([]uploadedFile, 0)
	for parts := 0; ; parts++ {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			bodyError(w, err)
			return
		}
		if parts >= 100 {
			part.Close()
			invalidRequest(w, "at most 100 multipart parts are allowed")
			return
		}
		if part.FormName() == "" {
			part.Close()
			invalidRequest(w, "each part needs a field name")
			return
		}
		if part.FileName() == "" {
			data, err := io.ReadAll(part)
			part.Close()
			if err != nil {
				bodyError(w, err)
				return
			}
			fields[part.FormName()] = append(fields[part.FormName()], string(data))
			continue
		}
		hash := sha256.New()
		prefix := make([]byte, 512)
		n, err := io.ReadFull(part, prefix)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			part.Close()
			bodyError(w, err)
			return
		}
		hash.Write(prefix[:n])
		rest, err := io.Copy(hash, part)
		part.Close()
		if err != nil {
			bodyError(w, err)
			return
		}
		files = append(files, uploadedFile{Field: part.FormName(), Name: part.FileName(), Size: int64(n) + rest,
			ContentType: http.DetectContentType(prefix[:n]), SHA256: hex.EncodeToString(hash.Sum(nil))})
	}
	// Consume epilogues too so the size cap applies to the entire upload.
	if _, err := io.Copy(io.Discard, req.Body); err != nil {
		bodyError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"fields": fields, "files": files})
}
