package routes

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/TrueFix/httptoy/pkg/httpx"
	"github.com/andybalholm/brotli"
	"github.com/gorilla/mux"
)

func addProtocolRoutes(r *mux.Router) {
	r.HandleFunc("/negotiate", protocolNegotiate).Methods(http.MethodGet)
	r.HandleFunc("/language", protocolLanguage).Methods(http.MethodGet)
	r.HandleFunc("/problem/{status}", protocolProblem).Methods(http.MethodGet)
	r.HandleFunc("/sse/resume", protocolResume).Methods(http.MethodGet)
	r.HandleFunc("/expect-continue", protocolExpect).Methods(http.MethodPost)
	r.HandleFunc("/request-trailers", protocolTrailers).Methods(http.MethodPost)
	r.HandleFunc("/decompress/{coding}", protocolDecompress).Methods(http.MethodPost)
	r.HandleFunc("/validate/{format}", protocolValidate).Methods(http.MethodPost)
}

type protocolPreference struct {
	name   string
	q      int
	params map[string]string
}

var protocolWorkSlots = make(chan struct{}, 8)

// Compression windows and parser bookkeeping can exceed the byte payload size.
// Bound aggregate work as well as per-request wire/expanded bytes.
func protocolWork(w http.ResponseWriter, req *http.Request) (func(), bool) {
	if req.Context().Err() != nil {
		return nil, false
	}
	select {
	case protocolWorkSlots <- struct{}{}:
		return func() { <-protocolWorkSlots }, true
	default:
		protocolWorkRejections.Add(1)
		w.Header().Set("Retry-After", "1")
		httpx.JSON(w, 503, map[string]string{"error": "validation/decompression capacity exhausted"})
		return nil, false
	}
}

// Bound header parsing independently of the transport's header limit. Quality
// syntax is shared with Accept-Encoding; specificity is evaluated before q.
func protocolPreferences(value string, media bool) ([]protocolPreference, error) {
	if len(value) > 4096 || strings.Count(value, ",") >= 32 {
		return nil, fmt.Errorf("preference header exceeds 4096 bytes or 32 entries")
	}
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	var result []protocolPreference
	for _, entry := range strings.Split(value, ",") {
		name, params, err := mime.ParseMediaType(strings.TrimSpace(entry))
		if err != nil {
			return nil, fmt.Errorf("invalid preference header")
		}
		if media {
			a, b, ok := strings.Cut(name, "/")
			if !ok || a == "" || b == "" || (a == "*" && b != "*") {
				return nil, fmt.Errorf("invalid media range")
			}
		} else if name != "*" {
			for index, part := range strings.Split(name, "-") {
				if len(part) < 1 || len(part) > 8 {
					return nil, fmt.Errorf("invalid language range")
				}
				for _, c := range part {
					if !(c >= 'a' && c <= 'z' || index > 0 && c >= '0' && c <= '9') {
						return nil, fmt.Errorf("invalid language range")
					}
				}
			}
		}
		q := 1000
		if raw, ok := params["q"]; ok {
			qualities, valid := encodingQualities("x;q=" + raw)
			if !valid {
				return nil, fmt.Errorf("invalid preference quality")
			}
			q = qualities["x"]
			delete(params, "q")
		}
		result = append(result, protocolPreference{name, q, params})
	}
	return result, nil
}

func protocolNegotiate(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Vary", "Accept")
	prefs, err := protocolPreferences(strings.Join(req.Header.Values("Accept"), ","), true)
	if err != nil {
		invalidRequest(w, err.Error())
		return
	}
	types := []string{"application/json", "application/xml", "text/html", "text/plain"}
	chosen, best := -1, 0
	for i, candidate := range types {
		q, specificity := 0, -1
		if len(prefs) == 0 {
			q = 1000
		}
		for _, p := range prefs {
			s := -1
			if p.name == "*/*" {
				s = 0
			} else if p.name == strings.SplitN(candidate, "/", 2)[0]+"/*" {
				s = 1
			} else if p.name == candidate {
				s = 2
			}
			for key, value := range p.params {
				if key != "charset" || !strings.EqualFold(value, "utf-8") {
					s = -1
				}
			}
			if s >= 0 {
				s = s*10 + len(p.params)
			}
			if s >= 0 && (s > specificity || s == specificity && p.q > q) {
				specificity, q = s, p.q
			}
		}
		if q > best {
			chosen, best = i, q
		}
	}
	if chosen < 0 {
		httpx.JSON(w, 406, map[string]string{"error": "no acceptable representation"})
		return
	}
	payloads := []string{`{"message":"Hello, world!"}`, `<message>Hello, world!</message>`, `<!doctype html><html><body>Hello, world!</body></html>`, "Hello, world!\n"}
	httpx.Data(w, 200, types[chosen]+"; charset=utf-8", []byte(payloads[chosen]))
}

func protocolLanguage(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Vary", "Accept-Language")
	prefs, err := protocolPreferences(strings.Join(req.Header.Values("Accept-Language"), ","), false)
	if err != nil {
		invalidRequest(w, err.Error())
		return
	}
	chosen, best := "", 0
	for _, language := range []string{"en", "es", "fr"} {
		q, specificity := 0, -1
		if len(prefs) == 0 {
			q = 1000
		}
		for _, p := range prefs {
			s := -1
			if p.name == "*" {
				s = 0
			} else if p.name == language {
				s = 2
			} else if p.q > 0 && strings.HasPrefix(p.name, language+"-") {
				// A regional exclusion does not exclude the generic language.
				s = 1
			}
			if len(p.params) != 0 {
				s = -1
			}
			if s >= 0 && (s > specificity || s == specificity && p.q > q) {
				specificity, q = s, p.q
			}
		}
		if q > best {
			chosen, best = language, q
		}
	}
	if chosen == "" {
		httpx.JSON(w, 406, map[string]string{"error": "no acceptable language"})
		return
	}
	w.Header().Set("Content-Language", chosen)
	httpx.JSON(w, 200, map[string]string{"language": chosen, "message": map[string]string{"en": "Hello!", "es": "¡Hola!", "fr": "Bonjour!"}[chosen]})
}

func protocolProblem(w http.ResponseWriter, req *http.Request) {
	status, err := strconv.Atoi(mux.Vars(req)["status"])
	if err != nil || status < 400 || status > 599 || http.StatusText(status) == "" {
		invalidRequest(w, "status must be a recognized HTTP error status (400–599)")
		return
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, status, map[string]any{"type": "about:blank", "title": http.StatusText(status), "status": status, "detail": "An example HTTP problem response."})
}

func protocolResume(w http.ResponseWriter, req *http.Request) {
	count, err := boundedNumber(req.URL.Query().Get("count"), 5, 0, 100)
	if err != nil {
		invalidRequest(w, err.Error())
		return
	}
	interval, err := boundedNumber(req.URL.Query().Get("interval_ms"), 100, 0, 1000)
	if err != nil || max(0, count-1)*interval > 10000 {
		invalidRequest(w, "interval_ms must be 0–1000 and total wait at most 10 seconds")
		return
	}
	last := int64(-1)
	if raw := req.Header.Get("Last-Event-ID"); raw != "" {
		last, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || last < 0 || last > (1<<63-1)-int64(count) {
			invalidRequest(w, "invalid Last-Event-ID")
			return
		}
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
		id := last + 1 + int64(i)
		if _, err := fmt.Fprintf(w, "id: %d\nevent: message\ndata: {\"id\":%d}\n\n", id, id); err != nil {
			return
		}
		if err := http.NewResponseController(w).Flush(); err != nil {
			return
		}
	}
}

func protocolRead(w http.ResponseWriter, req *http.Request) ([]byte, bool) {
	if req.ContentLength > maxBodyBytes {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		return nil, false
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, req.Body, maxBodyBytes))
	if err != nil {
		bodyError(w, err)
		return nil, false
	}
	return data, true
}

func protocolExpect(w http.ResponseWriter, req *http.Request) {
	// Never read or close the body here: reading would trigger net/http's automatic
	// 100 Continue. Connection close also prevents HTTP/1's post-handler drain.
	reject, err := strconv.ParseBool(defaultQuery(req, "reject", "false"))
	if err != nil {
		invalidRequest(w, "reject must be a boolean")
		return
	}
	if reject {
		if req.ProtoMajor == 1 {
			w.Header().Set("Connection", "close")
		}
		httpx.JSON(w, http.StatusExpectationFailed, map[string]string{"error": "expectation rejected before reading body"})
		return
	}
	data, ok := protocolRead(w, req)
	if !ok {
		return
	}
	httpx.JSON(w, 200, map[string]any{"bytes": len(data)})
}

func protocolTrailers(w http.ResponseWriter, req *http.Request) {
	data, ok := protocolRead(w, req)
	if !ok {
		return
	}
	// Trailer values become available only after the body reaches EOF.
	size := 0
	for key, values := range req.Trailer {
		size += len(key)
		for _, value := range values {
			size += len(value)
		}
		if size > 4096 || len(values) > 32 || len(req.Trailer) > 32 {
			invalidRequest(w, "trailers exceed 4096 bytes, 32 fields, or 32 values per field")
			return
		}
	}
	httpx.JSON(w, 200, map[string]any{"bytes": len(data), "trailers": req.Trailer})
}

func protocolDecompress(w http.ResponseWriter, req *http.Request) {
	coding := mux.Vars(req)["coding"]
	if coding != "gzip" && coding != "deflate" && coding != "br" {
		invalidRequest(w, "coding must be gzip, deflate, or br")
		return
	}
	release, ok := protocolWork(w, req)
	if !ok {
		return
	}
	defer release()
	// Read the entire capped wire payload first. Decoder read-ahead, concatenated
	// gzip members, and ignored trailing bytes cannot bypass the compressed cap.
	data, ok := protocolRead(w, req)
	if !ok {
		return
	}
	var reader io.Reader
	var closer io.Closer
	var err error
	switch coding {
	case "gzip":
		var r *gzip.Reader
		r, err = gzip.NewReader(bytes.NewReader(data))
		reader, closer = r, r
	case "deflate":
		reader, err = zlib.NewReader(bytes.NewReader(data))
		if err == nil {
			closer = reader.(io.Closer)
		}
	case "br":
		reader = brotli.NewReader(bytes.NewReader(data))
	}
	if err != nil {
		invalidRequest(w, "malformed compressed body")
		return
	}
	if closer != nil {
		defer closer.Close()
	}
	expanded, err := io.ReadAll(io.LimitReader(reader, maxBodyBytes+1))
	if len(expanded) > maxBodyBytes {
		w.WriteHeader(413)
		return
	}
	if err != nil {
		invalidRequest(w, "malformed compressed body or checksum")
		return
	}
	httpx.Data(w, 200, CONTENT_BYTES, expanded)
}

func protocolValidate(w http.ResponseWriter, req *http.Request) {
	format := mux.Vars(req)["format"]
	if format != "json" && format != "xml" && format != "multipart" {
		invalidRequest(w, "format must be json, xml, or multipart")
		return
	}
	release, ok := protocolWork(w, req)
	if !ok {
		return
	}
	defer release()
	data, ok := protocolRead(w, req)
	if !ok {
		return
	}
	var err error
	parts := 0
	switch format {
	case "json":
		decoder := json.NewDecoder(bytes.NewReader(data))
		var value json.RawMessage
		if err = decoder.Decode(&value); err != nil {
			err = fmt.Errorf("invalid JSON near byte %d", decoder.InputOffset())
		} else {
			if decoder.Decode(&value) != io.EOF {
				err = fmt.Errorf("JSON must contain exactly one top-level value")
			}
		}
	case "xml":
		err = protocolValidateXML(data)
	case "multipart":
		mediaType, params, parseErr := mime.ParseMediaType(req.Header.Get("Content-Type"))
		if parseErr != nil || !strings.HasPrefix(mediaType, "multipart/") || params["boundary"] == "" || len(params["boundary"]) > 70 {
			err = fmt.Errorf("multipart Content-Type requires a boundary of at most 70 bytes")
			break
		}
		reader := multipart.NewReader(bytes.NewReader(data), params["boundary"])
		for {
			part, nextErr := reader.NextRawPart()
			if nextErr == io.EOF {
				break
			}
			if nextErr != nil {
				err = fmt.Errorf("invalid multipart framing or headers at part %d", parts+1)
				break
			}
			parts++
			if parts > 100 {
				err = fmt.Errorf("multipart must contain at most 100 parts")
				break
			}
			if _, copyErr := io.Copy(io.Discard, part); copyErr != nil {
				err = fmt.Errorf("invalid multipart body at part %d", parts)
				break
			}
		}
		if parts == 0 && err == nil {
			err = fmt.Errorf("multipart requires at least one part")
		}
	}
	if err != nil {
		invalidRequest(w, err.Error())
		return
	}
	response := map[string]any{"valid": true, "format": format, "bytes": len(data)}
	if format == "multipart" {
		response["parts"] = parts
	}
	httpx.JSON(w, 200, response)
}

var xmlDeclarationPattern = regexp.MustCompile(`^version[ \t\r\n]*=[ \t\r\n]*(?:"1\.0"|'1\.0')(?:[ \t\r\n]+encoding[ \t\r\n]*=[ \t\r\n]*(?:"[A-Za-z][A-Za-z0-9._-]*"|'[A-Za-z][A-Za-z0-9._-]*'))?(?:[ \t\r\n]+standalone[ \t\r\n]*=[ \t\r\n]*(?:"(?:yes|no)"|'(?:yes|no)'))?[ \t\r\n]*$`)

func protocolValidateXML(data []byte) error {
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	decoder := xml.NewDecoder(bytes.NewReader(data))
	depth, roots := 0, 0
	for {
		offset := decoder.InputOffset()
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("invalid XML near byte %d", decoder.InputOffset())
		}
		switch value := token.(type) {
		case xml.StartElement:
			seen := make(map[xml.Name]bool, len(value.Attr))
			for _, attr := range value.Attr {
				if seen[attr.Name] {
					return fmt.Errorf("XML attributes must be unique")
				}
				seen[attr.Name] = true
			}
			if depth == 0 {
				roots++
			}
			depth++
			if depth > 128 || roots > 1 {
				return fmt.Errorf("XML requires one root and nesting at most 128")
			}
		case xml.EndElement:
			depth--
		case xml.CharData:
			if depth == 0 && len(bytes.TrimSpace(value)) != 0 {
				return fmt.Errorf("XML text outside root element")
			}
		case xml.Directive:
			return fmt.Errorf("XML directives and DTDs are not supported")
		case xml.ProcInst:
			if strings.EqualFold(value.Target, "xml") && (value.Target != "xml" || offset != 0 || !xmlDeclarationPattern.Match(value.Inst)) {
				return fmt.Errorf("XML declaration must be valid and occur only at the beginning")
			}
		}
	}
	if roots != 1 || depth != 0 {
		return fmt.Errorf("XML requires one complete root element")
	}
	return nil
}
