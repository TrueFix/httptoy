package routes

import (
	"net/http"

	"github.com/TrueFix/httptoy/pkg/httpx"
	"github.com/TrueFix/httptoy/pkg/parsers"
	"github.com/gorilla/mux"
)

func addAllRequestInspection(r *mux.Router) {
	r.HandleFunc("/headers", ri_headers).Methods("GET")
	r.HandleFunc("/header/{header_name}", ri_header_by_name).Methods("GET")
	r.HandleFunc("/ip", ri_client_real_ip).Methods("GET")
}
func ri_headers(w http.ResponseWriter, req *http.Request) {
	httpx.JSON(w, http.StatusOK, parsers.ParseHeaders(req))
}
func ri_header_by_name(w http.ResponseWriter, req *http.Request) {
	httpx.Data(w, http.StatusOK, CONTENT_TEXT, []byte(req.Header.Get(mux.Vars(req)["header_name"])))
}
func ri_client_real_ip(w http.ResponseWriter, req *http.Request) {
	httpx.Data(w, http.StatusOK, CONTENT_TEXT, []byte(httpx.ClientIP(req)))
}
