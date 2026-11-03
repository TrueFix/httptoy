package routes

import "testing"

func TestResourceAdmissionCounters(t *testing.T) {
	pool := func(name string) PoolStats {
		for _, stats := range ResourceSnapshot().Pools {
			if stats.Name == name {
				return stats
			}
		}
		t.Fatalf("missing pool %s", name)
		return PoolStats{}
	}
	beforeDelay, beforeWork := pool("delays"), pool("validation_decompression")
	for i := 0; i < cap(delaySlots); i++ {
		delaySlots <- struct{}{}
	}
	for i := 0; i < cap(protocolWorkSlots); i++ {
		protocolWorkSlots <- struct{}{}
	}
	defer func() {
		for len(delaySlots) > 0 {
			<-delaySlots
		}
		for len(protocolWorkSlots) > 0 {
			<-protocolWorkSlots
		}
	}()
	r := fixtureRouter()
	for _, path := range []string{"/delay/1", "/validate/json"} {
		method := "GET"
		if path == "/validate/json" {
			method = "POST"
		}
		if w := extendedRequest(r, method, path, nil, nil); w.Code != 503 {
			t.Fatalf("full pool %s: status=%d", path, w.Code)
		}
	}
	afterDelay, afterWork := pool("delays"), pool("validation_decompression")
	if afterDelay.Active != cap(delaySlots) || afterDelay.Capacity != cap(delaySlots) || afterDelay.Rejected != beforeDelay.Rejected+1 {
		t.Fatal("delay pool metrics incorrect")
	}
	if afterWork.Active != cap(protocolWorkSlots) || afterWork.Rejected != beforeWork.Rejected+1 {
		t.Fatal("validation/decompression metrics incorrect")
	}
}
