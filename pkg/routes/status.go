package routes

import (
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
)

func addAllStatus(r *mux.Router) {
	r.HandleFunc("/status/{code}", status_universal).Methods("GET", "POST", "DELETE", "PUT", "PATCH")
}
func status_universal(w http.ResponseWriter, req *http.Request) {
	code, err := strconv.Atoi(mux.Vars(req)["code"])
	if http.StatusText(code) == "" {
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(code)
}
