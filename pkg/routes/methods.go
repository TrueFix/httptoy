package routes

import (
	"mime"
	"net/http"

	"github.com/TrueFix/httptoy/pkg/httpx"
	"github.com/TrueFix/httptoy/pkg/parsers"
	"github.com/gorilla/mux"
)

func addAllMethods(r *mux.Router) {
	r.HandleFunc("/get", method_get).Methods("GET")
	r.HandleFunc("/post", method_with_body).Methods("POST")
	r.HandleFunc("/put", method_with_body).Methods("PUT")
	r.HandleFunc("/delete", method_with_body).Methods("DELETE")
	r.HandleFunc("/patch", method_with_body).Methods("PATCH")
}

type method_get_payload struct {
	Args    map[string]string `json:"args"`
	Headers map[string]string `json:"headers"`
	Origin  string            `json:"origin"`
	Url     string            `json:"url"`
}

func method_get(w http.ResponseWriter, req *http.Request) {
	httpx.JSON(w, http.StatusOK, requestSummary(req))
}

func requestSummary(req *http.Request) method_get_payload {
	return method_get_payload{Args: parsers.ParseParams(req), Headers: parsers.ParseHeaders(req), Origin: httpx.ClientIP(req), Url: parsers.ParseURL(req)}
}

func contentType(req *http.Request) string {
	value := req.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return value
	}
	return mediaType
}

type method_with_body_payload struct {
	Args       map[string]string      `json:"args"`
	Headers    map[string]string      `json:"headers"`
	Origin     string                 `json:"origin"`
	Url        string                 `json:"url"`
	Bodytype   string                 `json:"body_type"`
	BodyLength int64                  `json:"body_length"`
	Data       map[string]interface{} `json:"data"`
	Text       string                 `json:"text"`
}

func method_with_body(w http.ResponseWriter, req *http.Request) {
	req.Body = http.MaxBytesReader(w, req.Body, maxBodyBytes)
	data, text, err := parsers.ParseBodyDataWithError(req, contentType(req))
	if err != nil {
		bodyError(w, err)
		return
	}
	summary := requestSummary(req)
	httpx.JSON(w, http.StatusOK, method_with_body_payload{
		Args: summary.Args, Headers: summary.Headers, Origin: summary.Origin, Url: summary.Url,
		Bodytype: contentType(req), BodyLength: req.ContentLength, Data: data, Text: text,
	})
}
