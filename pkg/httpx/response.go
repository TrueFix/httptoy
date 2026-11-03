// Package httpx provides small net/http helpers shared by handlers and middleware.
package httpx

import (
	"encoding/json"
	"net/http"
)

// JSON encodes before committing headers so encoding errors can return 500.
func JSON(w http.ResponseWriter, status int, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	}
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

// Data writes a fixed response without overriding a deliberately supplied type.
func Data(w http.ResponseWriter, status int, contentType string, data []byte) {
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.WriteHeader(status)
	_, _ = w.Write(data)
}
