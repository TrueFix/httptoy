package routes

import (
	"context"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

var websocketSlots = make(chan struct{}, 64)

func websocketEcho(w http.ResponseWriter, req *http.Request) {
	select {
	case websocketSlots <- struct{}{}:
		defer func() { <-websocketSlots }()
	default:
		websocketRejections.Add(1)
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	// The library enforces same-origin browser requests. Non-browser clients may
	// omit Origin; cross-origin cookies are not needed for an echo service.
	conn, err := websocket.Accept(w, req, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(64 << 10)
	ctx, cancel := context.WithTimeout(req.Context(), 30*time.Second)
	defer cancel()
	for count := 0; count < 100; count++ {
		kind, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if err := conn.Write(ctx, kind, data); err != nil {
			return
		}
	}
	conn.Close(websocket.StatusNormalClosure, "message limit reached")
}
