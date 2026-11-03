package routes

import (
	"net/http"
	"strings"

	"github.com/TrueFix/httptoy/pkg/httpx"
	"github.com/gorilla/mux"
)

func addAllAuth(r *mux.Router) {
	r.HandleFunc("/basic/{username}/{password}", auth_basic_user_pass).Methods("GET")
	r.HandleFunc("/basic", auth_basic).Methods("GET")
	r.HandleFunc("/bearer", auth_bearer).Methods("GET")
}

func auth_basic_user_pass(w http.ResponseWriter, req *http.Request) {
	username, password := mux.Vars(req)["username"], mux.Vars(req)["password"]
	user, pass, ok := req.BasicAuth()
	if !ok || user != username || pass != password {
		w.Header().Set("WWW-Authenticate", "Basic realm=Restricted")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"username": username, "password": password, "auth": true})
}

func auth_basic(w http.ResponseWriter, req *http.Request) {
	user, pass, ok := req.BasicAuth()
	if !ok {
		w.Header().Set("WWW-Authenticate", "Basic realm=Restricted")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"username": user, "password": pass, "auth": true})
}

func auth_bearer(w http.ResponseWriter, req *http.Request) {
	authorization := req.Header.Get("Authorization")
	scheme, token, found := strings.Cut(authorization, " ")
	if found && strings.EqualFold(scheme, "Bearer") && validBearerToken(token) {
		httpx.JSON(w, http.StatusOK, map[string]any{"authenticated": true, "token": authorization})
		return
	}
	w.Header().Set("WWW-Authenticate", "Bearer")
	w.WriteHeader(http.StatusUnauthorized)
}

func validBearerToken(token string) bool {
	if token == "" {
		return false
	}
	padding := false
	for _, ch := range token {
		if ch == '=' {
			padding = true
			continue
		}
		if padding {
			return false
		}
		if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || strings.ContainsRune("-._~+/", ch) {
			continue
		}
		return false
	}
	return token[0] != '='
}
