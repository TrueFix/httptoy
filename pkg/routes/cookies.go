package routes

import (
	"net/http"
	"net/url"

	"github.com/TrueFix/httptoy/pkg/httpx"
	"github.com/gorilla/mux"
)

func addAllCookies(r *mux.Router) {
	r.HandleFunc("/cookies", cookies_default).Methods("GET")
	r.HandleFunc("/cookies/set/{key}/{value}", cookies_set_key_value).Methods("GET")
	r.HandleFunc("/cookies/delete/{key}", cookies_delete_key).Methods("GET")
}

func cookies_default(w http.ResponseWriter, req *http.Request) {
	cookies := make(map[string]string)
	for _, cookie := range req.Cookies() {
		cookies[cookie.Name] = cookie.Value
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"cookies": cookies})
}

func cookies_set_key_value(w http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	http.SetCookie(w, &http.Cookie{Name: vars["key"], Value: url.QueryEscape(vars["value"]), Path: "/"})
	http.Redirect(w, req, "/cookies", http.StatusFound)
}

func cookies_delete_key(w http.ResponseWriter, req *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: mux.Vars(req)["key"], Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, req, "/cookies", http.StatusFound)
}
