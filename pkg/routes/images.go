package routes

import (
	"net/http"

	"github.com/TrueFix/httptoy/pkg/httpx"
	"github.com/TrueFix/httptoy/pkg/parsers"
	"github.com/gorilla/mux"
)

var supportedTypes = []string{CONTENT_IMAGE_WEBP, CONTENT_IMAGE_SVG, CONTENT_IMAGE_JPEG, CONTENT_IMAGE_PNG, "image/*"}

func addAllImages(r *mux.Router) {
	images := []struct{ extension, contentType string }{
		{"webp", CONTENT_IMAGE_WEBP}, {"svg", CONTENT_IMAGE_SVG},
		{"jpg", CONTENT_IMAGE_JPEG}, {"png", CONTENT_IMAGE_PNG},
	}
	handlers := make(map[string]http.HandlerFunc, len(images))
	for _, image := range images {
		handler := fixtureHandler("images/sample."+image.extension, image.contentType)
		handlers[image.contentType] = handler
		path := image.extension
		if path == "jpg" {
			path = "jpeg"
		}
		r.HandleFunc("/image/"+path, handler).Methods("GET")
	}
	r.HandleFunc("/image", func(w http.ResponseWriter, req *http.Request) {
		selected := parsers.ParseSupportedImage(req, supportedTypes[:len(supportedTypes)-1])
		if handler, ok := handlers[selected]; ok {
			handler(w, req)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{
			"message": "Accept header not found or supported type not found",
			"support": supportedTypes,
		})
	}).Methods("GET")
}
