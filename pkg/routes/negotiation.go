package routes

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/TrueFix/httptoy/pkg/httpx"
)

// encodingQualities uses integer thousandths to avoid floating-point edge cases
// and enforces RFC qvalue syntax (0–1, at most three fractional digits).
func encodingQualities(value string) (map[string]int, bool) {
	qualities := make(map[string]int)
	if strings.TrimSpace(value) == "" {
		return qualities, true
	}
	entries := strings.Split(value, ",")
	if len(entries) > 32 || len(value) > 4096 {
		return nil, false
	}
	for _, entry := range entries {
		parts := strings.Split(entry, ";")
		coding := strings.ToLower(strings.TrimSpace(parts[0]))
		if !validToken(coding) || len(parts) > 2 {
			return nil, false
		}
		quality := 1000
		if len(parts) == 2 {
			key, q, found := strings.Cut(strings.TrimSpace(parts[1]), "=")
			if !found || !strings.EqualFold(strings.TrimSpace(key), "q") {
				return nil, false
			}
			q = strings.TrimSpace(q)
			whole, fraction, decimal := strings.Cut(q, ".")
			if whole != "0" && whole != "1" || len(fraction) > 3 {
				return nil, false
			}
			for _, ch := range fraction {
				if ch < '0' || ch > '9' || whole == "1" && ch != '0' {
					return nil, false
				}
			}
			quality = 0
			if whole == "1" {
				quality = 1000
			} else if decimal && fraction != "" {
				quality, _ = strconv.Atoi(fraction + strings.Repeat("0", 3-len(fraction)))
			}
		}
		if previous, exists := qualities[coding]; !exists || quality > previous {
			qualities[coding] = quality
		}
	}
	return qualities, true
}

func negotiateCompression(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Vary", "Accept-Encoding")
	qualities, ok := encodingQualities(strings.Join(req.Header.Values("Accept-Encoding"), ","))
	if !ok {
		invalidRequest(w, "invalid Accept-Encoding; at most 32 codings and qvalues with three decimal places")
		return
	}
	chosen, best := "", 0
	for _, coding := range []string{"br", "gzip", "deflate", "identity"} {
		quality, explicit := qualities[coding]
		if !explicit {
			if coding == "identity" {
				quality = 1000
				if wildcard, exists := qualities["*"]; exists && wildcard == 0 {
					quality = 0
				}
			} else {
				quality = qualities["*"]
			}
		}
		if quality > best {
			chosen, best = coding, quality
		}
	}
	if chosen == "" {
		httpx.JSON(w, http.StatusNotAcceptable, map[string]any{"error": "no acceptable content coding"})
		return
	}
	payload := []byte(DEFAULT_STRING)
	switch chosen {
	case "br":
		payload = brotliPayload
	case "gzip":
		payload = gzipPayload
	case "deflate":
		payload = deflatePayload
	}
	if chosen != "identity" {
		w.Header().Set("Content-Encoding", chosen)
	}
	httpx.Data(w, http.StatusOK, CONTENT_TEXT, payload)
}
