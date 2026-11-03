package routes

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/TrueFix/httptoy/pkg/httpx"
)

func configurableCookies(w http.ResponseWriter, req *http.Request) {
	if len(req.URL.RawQuery) > 16384 {
		invalidRequest(w, "cookie query exceeds 16 KiB")
		return
	}
	values, err := parseCookieOptions(req)
	if err != nil {
		invalidRequest(w, err.Error())
		return
	}
	names := make([]string, 0, len(values))
	for _, cookie := range values {
		http.SetCookie(w, cookie)
		names = append(names, cookie.Name)
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, map[string]any{"set": names})
}

func parseCookieOptions(req *http.Request) ([]*http.Cookie, error) {
	// Validate every cookie before emitting any Set-Cookie headers.
	secure, err := strconv.ParseBool(defaultQuery(req, "secure", "false"))
	if err != nil {
		return nil, cookieOptionError("secure must be a boolean")
	}
	httpOnly, err := strconv.ParseBool(defaultQuery(req, "httponly", "false"))
	if err != nil {
		return nil, cookieOptionError("httponly must be a boolean")
	}
	age, err := boundedNumber(req.URL.Query().Get("max_age"), 0, -1, 31536000)
	if err != nil {
		return nil, err
	}
	sameSite := http.SameSiteDefaultMode
	switch strings.ToLower(defaultQuery(req, "samesite", "default")) {
	case "default":
	case "lax":
		sameSite = http.SameSiteLaxMode
	case "strict":
		sameSite = http.SameSiteStrictMode
	case "none":
		if !secure {
			return nil, cookieOptionError("SameSite=None requires secure=true")
		}
		sameSite = http.SameSiteNoneMode
	default:
		return nil, cookieOptionError("samesite must be default, lax, strict, or none")
	}
	var expires time.Time
	if value := req.URL.Query().Get("expires"); value != "" {
		expires, err = time.Parse(time.RFC3339, value)
		if err != nil {
			return nil, cookieOptionError("expires must be RFC 3339")
		}
		expires = expires.UTC()
	}
	path, domain := defaultQuery(req, "path", "/"), req.URL.Query().Get("domain")
	if !strings.HasPrefix(path, "/") {
		return nil, cookieOptionError("path must start with /")
	}
	reserved := map[string]bool{"secure": true, "httponly": true, "max_age": true, "samesite": true, "expires": true, "path": true, "domain": true}
	query := req.URL.Query()
	names := make([]string, 0, len(query))
	for name := range query {
		if !reserved[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	cookies := make([]*http.Cookie, 0)
	total := 0
	for _, name := range names {
		if strings.HasPrefix(name, "__Secure-") && !secure {
			return nil, cookieOptionError("__Secure- cookies require secure=true")
		}
		if strings.HasPrefix(name, "__Host-") && (!secure || path != "/" || domain != "") {
			return nil, cookieOptionError("__Host- cookies require Secure, path=/, and no domain")
		}
		for _, value := range query[name] {
			cookie := &http.Cookie{Name: name, Value: value, Path: path, Domain: domain, Secure: secure, HttpOnly: httpOnly, SameSite: sameSite, MaxAge: age, Expires: expires}
			if err := cookie.Valid(); err != nil {
				return nil, cookieOptionError("invalid cookie name, value, or attributes")
			}
			length := len(cookie.String())
			total += length
			if length > 4096 || total > 16384 || len(cookies) >= 16 {
				return nil, cookieOptionError("at most 16 cookies, 4 KiB each, 16 KiB total are allowed")
			}
			cookies = append(cookies, cookie)
		}
	}
	if len(cookies) == 0 {
		return nil, cookieOptionError("provide at least one name=value cookie")
	}
	return cookies, nil
}

type cookieOptionError string

func (e cookieOptionError) Error() string { return string(e) }

func inspectCookies(w http.ResponseWriter, req *http.Request) {
	parsed := make([]map[string]string, 0)
	for _, cookie := range req.Cookies() {
		parsed = append(parsed, map[string]string{"name": cookie.Name, "value": cookie.Value})
	}
	raw := req.Header.Values("Cookie")
	if raw == nil {
		raw = []string{}
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, map[string]any{"raw": raw, "cookies": parsed})
}
