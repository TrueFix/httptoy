package routes

import (
	"net/http"
	"strconv"
	"time"
)

const maxDelaySeconds = 10

// waitForDelay validates before converting to a duration, avoiding overflow.
// The shared admission budget bounds outstanding waits without blocking admission.
var delaySlots = make(chan struct{}, 128)

func waitForDelay(w http.ResponseWriter, req *http.Request, value string) bool {
	seconds := delaySeconds(value)
	if seconds < 0 {
		w.WriteHeader(http.StatusBadRequest)
		return false
	}
	if req.Context().Err() != nil {
		return false
	}
	if seconds == 0 {
		return true
	}
	select {
	case delaySlots <- struct{}{}:
		defer func() { <-delaySlots }()
	default:
		delayRejections.Add(1)
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusServiceUnavailable)
		return false
	}
	timer := time.NewTimer(time.Duration(seconds) * time.Second)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-req.Context().Done():
		return false
	}
}

func delaySeconds(value string) int {
	seconds, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	if seconds > maxDelaySeconds {
		return maxDelaySeconds
	}
	return seconds
}
