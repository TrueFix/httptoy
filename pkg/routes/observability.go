package routes

import "sync/atomic"

var delayRejections, streamRejections, websocketRejections, protocolWorkRejections, sessionRejections atomic.Uint64
var sseClients atomic.Int64

// PoolStats contains only fixed pool names and aggregate counts, never IDs.
type PoolStats struct {
	Name             string
	Active, Capacity int
	Rejected         uint64
}
type ResourceStats struct {
	Pools                                                                       []PoolStats
	SessionsActive, CaptureBytes, CapturedRequests, IdempotencyKeys, SSEClients int
}

// ResourceSnapshot takes a short store lock, then returns independent scalars.
// No caller formats or sends a scrape while holding the store lock.
func ResourceSnapshot() ResourceStats {
	stats := ResourceStats{SSEClients: int(sseClients.Load())}
	workflowStore.Lock()
	workflowStore.prune()
	stats.SessionsActive = len(workflowStore.entries)
	for _, session := range workflowStore.entries {
		stats.CaptureBytes += session.storedBytes
		stats.CapturedRequests += len(session.requests)
		stats.IdempotencyKeys += len(session.idempotency)
	}
	workflowStore.Unlock()
	stats.Pools = []PoolStats{
		{Name: "delays", Active: len(delaySlots), Capacity: cap(delaySlots), Rejected: delayRejections.Load()},
		{Name: "streams", Active: len(streamSlots), Capacity: cap(streamSlots), Rejected: streamRejections.Load()},
		{Name: "websockets", Active: len(websocketSlots), Capacity: cap(websocketSlots), Rejected: websocketRejections.Load()},
		{Name: "validation_decompression", Active: len(protocolWorkSlots), Capacity: cap(protocolWorkSlots), Rejected: protocolWorkRejections.Load()},
		{Name: "sessions", Active: stats.SessionsActive, Capacity: maxSessions, Rejected: sessionRejections.Load()},
	}
	return stats
}
