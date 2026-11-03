package routes

import (
	"net/http"

	"github.com/TrueFix/httptoy/pkg/httpx"
	samplefiles "github.com/TrueFix/httptoy/sample_files"
)

// fixtureHandler loads and validates immutable data once, when routes are built.
func fixtureHandler(name, contentType string) http.HandlerFunc {
	data, err := samplefiles.Files.ReadFile(name)
	if err != nil {
		panic(err)
	}
	return func(w http.ResponseWriter, req *http.Request) {
		httpx.Data(w, http.StatusOK, contentType, data)
	}
}
