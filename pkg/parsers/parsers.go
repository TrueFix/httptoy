package parsers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type dict map[string]string

func ParseHeaders(req *http.Request) dict {
	return joinValues(req.Header)
}

func ParseParams(req *http.Request) dict {
	return joinValues(req.URL.Query())
}

func joinValues(values map[string][]string) dict {
	result := make(dict, len(values))
	for key, value := range values {
		result[key] = strings.Join(value, ";")
	}
	return result
}

func ParseURL(req *http.Request) string {
	var scheme string = "http"
	if req.TLS != nil {
		scheme = "https"
	}

	return scheme + "://" + req.Host + req.URL.RequestURI()
}

// ParseBodyData preserves the legacy string-only API. New callers should use
// ParseBodyDataWithError to retain JSON value types and handle invalid bodies.
func ParseBodyData(req *http.Request, ct string) (dict, string) {
	data, text, _ := ParseBodyDataWithError(req, ct)
	legacy := make(dict)
	for key, value := range data {
		if str, ok := value.(string); ok {
			legacy[key] = str
		}
	}
	return legacy, text
}

// ParseBodyDataWithError parses form data, JSON objects, and text. JSON object
// values retain their types, including nested objects, arrays, and nulls.
// Callers are responsible for bounding req.Body (for example, MaxBytesReader).
func ParseBodyDataWithError(req *http.Request, ct string) (map[string]interface{}, string, error) {
	data := make(map[string]interface{})
	if req.Body == nil {
		req.Body = http.NoBody
	}
	// Read even unsupported bodies so a caller's size limit always applies,
	// including chunked bodies and multipart epilogues.
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return data, "", err
	}
	if ct == "" {
		return data, "", nil
	}
	mediaType, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return data, "", err
	}
	var values map[string][]string
	switch mediaType {
	case "application/x-www-form-urlencoded":
		values, err = url.ParseQuery(string(body))
	case "multipart/form-data":
		req.Body = io.NopCloser(bytes.NewReader(body))
		defer func() {
			if req.MultipartForm != nil {
				req.MultipartForm.RemoveAll()
			}
		}()
		// Echo handlers cap the entire body at 1 MiB. Keep those bounded files
		// in memory so the scratch container needs no writable temporary dir.
		err = req.ParseMultipartForm(1 << 20)
		values = req.PostForm
	case "text/plain", "text/html":
		return data, string(body), nil
	case "application/json":
		err = json.Unmarshal(body, &data)
		if err == nil && data == nil {
			err = fmt.Errorf("JSON body must be an object")
		}
	}
	if err != nil {
		return nil, "", err
	}
	for key, value := range joinValues(values) {
		data[key] = value
	}
	return data, "", nil
}

func ParseSupportedImage(req *http.Request, supportedTypes []string) string {
	bestType, bestQuality := "", 0.0
	for _, supported := range supportedTypes {
		quality, specificity := 0.0, -1
		for _, header := range req.Header.Values("Accept") {
			for _, entry := range strings.Split(header, ",") {
				mediaType, params, err := mime.ParseMediaType(strings.TrimSpace(entry))
				if err != nil {
					continue
				}
				match := -1
				switch {
				case mediaType == supported:
					match = 2
				case mediaType == strings.SplitN(supported, "/", 2)[0]+"/*":
					match = 1
				case mediaType == "*/*":
					match = 0
				}
				if match < 0 {
					continue
				}
				q := 1.0
				if raw, exists := params["q"]; exists {
					q, err = strconv.ParseFloat(raw, 64)
					if err != nil || !(q >= 0 && q <= 1) {
						continue
					}
				}
				if match > specificity || (match == specificity && q > quality) {
					quality, specificity = q, match
				}
			}
		}
		if quality > bestQuality {
			bestType, bestQuality = supported, quality
		}
	}
	return bestType
}
