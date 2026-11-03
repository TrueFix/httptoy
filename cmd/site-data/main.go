// Command site-data exports the actual router inventory for the pre-rendered site.
package main

import (
	"encoding/json"
	"log"
	"net/http/httptest"
	"os"

	"github.com/TrueFix/httptoy/pkg/server"
)

func main() {
	h := server.Server()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/openapi.json", nil))
	if w.Code != 200 {
		log.Fatal("cannot export API inventory")
	}
	var document any
	if err := json.Unmarshal(w.Body.Bytes(), &document); err != nil {
		log.Fatal(err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(document); err != nil {
		log.Fatal(err)
	}
}
