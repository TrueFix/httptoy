package routes

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
)

func addAllRequestRedirect(r *mux.Router) {
	r.HandleFunc("/redirect/{times}", redirect_redirect_times).Methods("GET")
	r.HandleFunc("/redirect-delay/{delay}/{times}", redirect_redirect_delay_times).Methods("GET")
}

func redirect_redirect_times(w http.ResponseWriter, req *http.Request) {
	timeString := mux.Vars(req)["times"]
	times, err := strconv.Atoi(timeString)

	if err != nil {
		times = 0
	}

	if times <= 1 {
		http.Redirect(w, req, "/get", http.StatusFound)
	} else {
		http.Redirect(w, req, fmt.Sprintf("/redirect/%d", times-1), http.StatusFound)
	}

}

func redirect_redirect_delay_times(w http.ResponseWriter, req *http.Request) {
	delayString := mux.Vars(req)["delay"]
	timeString := mux.Vars(req)["times"]

	times, err := strconv.Atoi(timeString)
	if err != nil {
		times = 0
	}

	if !waitForDelay(w, req, delayString) {
		return
	}

	if times <= 1 {
		http.Redirect(w, req, "/get", http.StatusFound)
	} else {
		http.Redirect(w, req, fmt.Sprintf("/redirect-delay/%s/%d", delayString, times-1), http.StatusFound)
	}

}
