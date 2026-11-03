package routes

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/TrueFix/httptoy/pkg/httpx"
)

const digestRealm = "httptoy"
const digestUser = "user"
const digestPassword = "passwd"

var digestKey = func() []byte {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic(err)
	}
	return key
}()

// Successful nonce counts are retained until their signed nonce expires. The
// fixed capacity bounds memory without evicting replay-protection entries early.
var digestReplay = struct {
	sync.Mutex
	entries map[string]digestCount
}{entries: make(map[string]digestCount)}

type digestCount struct {
	count   uint64
	expires int64
}

func signedNonce(now time.Time) string {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		panic(err)
	}
	payload := strconv.FormatInt(now.Unix(), 10) + ":" + hex.EncodeToString(random)
	mac := hmac.New(sha256.New, digestKey)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString([]byte(payload + ":" + hex.EncodeToString(mac.Sum(nil))))
}

func nonceExpiry(nonce string, now time.Time) (int64, bool) {
	decoded, err := base64.RawURLEncoding.DecodeString(nonce)
	if err != nil {
		return 0, false
	}
	parts := strings.Split(string(decoded), ":")
	if len(parts) != 3 || len(parts[1]) != 32 {
		return 0, false
	}
	timestamp, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || timestamp > now.Unix() || timestamp < now.Unix()-300 {
		return 0, false
	}
	signature, err := hex.DecodeString(parts[2])
	if err != nil {
		return 0, false
	}
	mac := hmac.New(sha256.New, digestKey)
	mac.Write([]byte(parts[0] + ":" + parts[1]))
	return timestamp + 300, hmac.Equal(signature, mac.Sum(nil))
}

func digestChallenge(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Digest realm="%s", nonce="%s", algorithm=SHA-256, qop="auth", charset=UTF-8`, digestRealm, signedNonce(time.Now())))
	w.WriteHeader(http.StatusUnauthorized)
}

func digestAuth(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	params, ok := parseDigest(req.Header.Get("Authorization"))
	if !ok {
		digestChallenge(w)
		return
	}
	expires, nonceOK := nonceExpiry(params["nonce"], time.Now())
	nc, ncErr := strconv.ParseUint(params["nc"], 16, 32)
	if !nonceOK || params["username"] != digestUser || params["realm"] != digestRealm ||
		!strings.EqualFold(params["algorithm"], "SHA-256") || params["qop"] != "auth" ||
		params["uri"] != req.URL.RequestURI() || len(params["nc"]) != 8 || ncErr != nil || nc == 0 ||
		params["cnonce"] == "" || len(params["cnonce"]) > 128 {
		digestChallenge(w)
		return
	}
	ha1 := checksum([]byte(digestUser + ":" + digestRealm + ":" + digestPassword))
	ha2 := checksum([]byte(req.Method + ":" + params["uri"]))
	expected, _ := hex.DecodeString(checksum([]byte(ha1 + ":" + params["nonce"] + ":" + params["nc"] + ":" + params["cnonce"] + ":auth:" + ha2)))
	actual, err := hex.DecodeString(params["response"])
	if err != nil || subtle.ConstantTimeCompare(actual, expected) != 1 {
		digestChallenge(w)
		return
	}
	key := params["nonce"] + ":" + params["cnonce"]
	digestReplay.Lock()
	for key, entry := range digestReplay.entries {
		if entry.expires < time.Now().Unix() {
			delete(digestReplay.entries, key)
		}
	}
	previous, exists := digestReplay.entries[key]
	if exists && previous.count >= nc {
		digestReplay.Unlock()
		digestChallenge(w)
		return
	}
	if !exists && len(digestReplay.entries) >= 4096 {
		digestReplay.Unlock()
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	digestReplay.entries[key] = digestCount{count: nc, expires: expires}
	digestReplay.Unlock()
	httpx.JSON(w, http.StatusOK, map[string]any{"authenticated": true, "username": digestUser})
}

// parseDigest handles quoted commas and escaped quotes, rejects duplicates, and
// bounds header processing. Unknown extension attributes are harmless.
func parseDigest(header string) (map[string]string, bool) {
	scheme, rest, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "Digest") || len(header) > 8192 {
		return nil, false
	}
	params := make(map[string]string)
	for rest = strings.TrimSpace(rest); rest != ""; {
		name, tail, found := strings.Cut(rest, "=")
		name = strings.ToLower(strings.TrimSpace(name))
		if !found || !validToken(name) {
			return nil, false
		}
		if _, duplicate := params[name]; duplicate {
			return nil, false
		}
		tail = strings.TrimSpace(tail)
		var value string
		if strings.HasPrefix(tail, "\"") {
			var b strings.Builder
			closed := false
			for i := 1; i < len(tail); i++ {
				if tail[i] == '"' {
					value, tail, closed = b.String(), tail[i+1:], true
					break
				}
				if tail[i] == '\\' {
					i++
					if i >= len(tail) {
						return nil, false
					}
				}
				b.WriteByte(tail[i])
			}
			if !closed {
				return nil, false
			}
		} else {
			var comma bool
			value, tail, comma = strings.Cut(tail, ",")
			value = strings.TrimSpace(value)
			if !validToken(value) {
				return nil, false
			}
			if comma {
				tail = "," + tail
			}
		}
		if !validHeaderValue(value) {
			return nil, false
		}
		params[name] = value
		rest = strings.TrimSpace(tail)
		if rest == "" {
			break
		}
		if rest[0] != ',' {
			return nil, false
		}
		rest = strings.TrimSpace(rest[1:])
		if rest == "" {
			return nil, false
		}
	}
	return params, len(params) > 0
}
