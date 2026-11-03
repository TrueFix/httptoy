package routes

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strconv"

	"github.com/TrueFix/httptoy/pkg/httpx"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

func addAllResponse(r *mux.Router) {
	r.HandleFunc("/bytes/{count}", response_bytes_count).Methods("GET")
	r.HandleFunc("/base64en/{data:.+}", response_base64en).Methods("GET")
	r.HandleFunc("/base64de/{data:.+}", response_base64de).Methods("GET")

	r.HandleFunc("/delay/{seconds}", response_delay).Methods("GET", "POST", "DELETE", "PUT", "PATCH")

	r.HandleFunc("/uuid", response_uuid).Methods("GET")
	r.HandleFunc("/body/{data}", response_body_data).Methods("GET")

	r.HandleFunc("/text", fixtureHandler("data/sample.text", CONTENT_TEXT)).Methods("GET")
	r.HandleFunc("/json", fixtureHandler("data/sample.json", CONTENT_JSON)).Methods("GET")
	r.HandleFunc("/xml", fixtureHandler("data/sample.xml", CONTENT_XML)).Methods("GET")
	r.HandleFunc("/yaml", fixtureHandler("data/sample.yaml", CONTENT_YAML)).Methods("GET")
	r.HandleFunc("/html", fixtureHandler("data/sample.html", CONTENT_HTML)).Methods("GET")

}

func response_bytes_count(w http.ResponseWriter, req *http.Request) {
	countString := mux.Vars(req)["count"]
	count, err := strconv.Atoi(countString)

	if err != nil {
		count = 1024
	}

	if count > 1024 {
		count = 1024
	}
	if count < 0 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	randomBytes := make([]byte, count)
	if _, err := rand.Read(randomBytes); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	httpx.Data(w, http.StatusOK, CONTENT_BYTES, randomBytes)
}

func response_base64en(w http.ResponseWriter, req *http.Request) {
	dat := mux.Vars(req)["data"]
	rawDecodedText := base64.StdEncoding.EncodeToString([]byte(dat))
	httpx.Data(w, http.StatusOK, CONTENT_TEXT, []byte(rawDecodedText))
}

func response_base64de(w http.ResponseWriter, req *http.Request) {
	dat := mux.Vars(req)["data"]
	rawDecodedText, err := base64.StdEncoding.DecodeString(dat)

	if err != nil {
		httpx.Data(w, http.StatusOK, CONTENT_TEXT, []byte(INVALID_DATA))
		return
	}

	httpx.Data(w, http.StatusOK, CONTENT_TEXT, []byte(rawDecodedText))
}
func response_delay(w http.ResponseWriter, req *http.Request) {
	if !waitForDelay(w, req, mux.Vars(req)["seconds"]) {
		return
	}
	httpx.JSON(w, http.StatusOK, requestSummary(req))
}

func response_uuid(w http.ResponseWriter, req *http.Request) {
	uid := uuid.New()
	httpx.Data(w, http.StatusOK, CONTENT_TEXT, []byte(uid.String()))
}
func response_body_data(w http.ResponseWriter, req *http.Request) {
	dat := mux.Vars(req)["data"]
	httpx.Data(w, http.StatusOK, CONTENT_TEXT, []byte(dat))
}
