package server

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/TrueFix/httptoy/pkg/httpx"
	"github.com/TrueFix/httptoy/pkg/routes"
	"github.com/TrueFix/httptoy/templates"
	"github.com/gorilla/mux"
)

var hashedAsset = regexp.MustCompile(`-[A-Za-z0-9_-]{8,}\.`)

func publicURL() string {
	value := strings.TrimRight(os.Getenv("PUBLIC_URL"), "/")
	if value == "" {
		value = "https://httptoy.com"
	}
	u, err := url.Parse(value)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || (u.Scheme != "https" && u.Scheme != "http") {
		panic("PUBLIC_URL must be an absolute HTTP(S) origin without a path, query, or credentials")
	}
	return value
}

func registerWebsite(r *mux.Router) {
	siteURL := publicURL()
	schema := map[string]any{"@context": "https://schema.org", "@type": "SoftwareApplication", "name": "httptoy", "url": siteURL + "/", "description": "An open-source HTTP testing API for request inspection, retries, streaming, webhooks, and protocol testing.", "applicationCategory": "DeveloperApplication", "operatingSystem": "Linux, macOS, Windows", "isAccessibleForFree": true, "license": "https://github.com/TrueFix/httptoy/blob/main/LICENSE", "codeRepository": "https://github.com/TrueFix/httptoy"}
	structured, err := json.Marshal(schema)
	if err != nil {
		panic(err)
	}
	source, err := templates.Files.ReadFile("index.html")
	if err != nil {
		panic(err)
	}
	// html/template strips literal comments, but React needs these text boundaries.
	page := template.Must(template.New("index.html").Parse(strings.ReplaceAll(string(source), "<!-- -->", "{{.ReactSeparator}}")))
	var index bytes.Buffer
	if err := page.ExecuteTemplate(&index, "index.html", map[string]any{"SiteURL": siteURL, "CanonicalURL": siteURL + "/", "SocialImage": siteURL + "/assets/social.png", "StructuredData": template.JS(structured), "ReactSeparator": template.HTML("<!-- -->")}); err != nil {
		panic(err)
	}
	r.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		httpx.Data(w, 200, routes.CONTENT_HTML, index.Bytes())
	}).Methods("GET", "HEAD")
	web, err := fs.Sub(templates.Files, "web")
	if err != nil {
		panic(err)
	}
	files := http.FileServer(http.FS(web))
	r.PathPrefix("/assets/").Handler(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		name := strings.TrimPrefix(req.URL.Path, "/")
		if !fs.ValidPath(name) || strings.HasSuffix(name, "/") {
			http.NotFound(w, req)
			return
		}
		if hashedAsset.MatchString(name) {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=3600")
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		files.ServeHTTP(w, req)
	})).Methods("GET", "HEAD")
	r.GetRoute("robots").HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		httpx.Data(w, 200, routes.CONTENT_TEXT, []byte("User-agent: *\nAllow: /\nDisallow: /deny\nSitemap: "+siteURL+"/sitemap.xml\n"))
	})
	var location bytes.Buffer
	if err := xml.EscapeText(&location, []byte(siteURL+"/")); err != nil {
		panic(err)
	}
	sitemap := []byte(`<?xml version="1.0" encoding="UTF-8"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>` + location.String() + `</loc></url></urlset>`)
	r.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		httpx.Data(w, 200, routes.CONTENT_XML, sitemap)
	}).Methods("GET", "HEAD")
}

func websiteHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/" && req.URL.Path != "/robots.txt" && req.URL.Path != "/sitemap.xml" && !strings.HasPrefix(req.URL.Path, "/assets/") {
			w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		}
		next.ServeHTTP(w, req)
	})
}
