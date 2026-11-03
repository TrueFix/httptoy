# Performance checks

Run correctness and race checks before benchmarking:

```sh
go test -race ./...
go test ./pkg/routes -run '^$' -bench . -benchmem -count 3
```

The benchmarks include Gorilla mux routing and response-recorder allocations, not socket
I/O or production logging. Compare the same Go version, hardware, and logging mode.
Compression payloads are immutable and precomputed at startup; fixture handlers
load embedded bytes when routes are registered. Neither performs filesystem I/O
or recompression on the request path.

## Bounded deployment validation

`GET /metrics` provides request counts, handler-duration histograms, in-flight
handlers, pool occupancy/rejections and session/capture gauges. Filter intentional
delay/stream/fault routes out of ordinary-request SLO queries. Histograms include
full stream/WebSocket lifetimes, not network round-trip latency; capture-byte
accounting is not peak RSS. Protect metrics at the network/proxy boundary.

Use a dedicated local instance, never a shared public endpoint. Set a finite run
duration (for example 10 seconds), concurrency (start at 4, no more than 32), and
client timeout (15 seconds). Record p50/p95/p99, requests/sec, CPU, RSS, and open
connections for `/get`, `/brotli`, `/gzip`, `/text`, and `/image/png` separately.

Then run ordinary traffic alongside at most 16 `/delay/1` requests. Compare the
ordinary-route p95; exclude intentional wait time from that comparison. Cancel
delayed clients and verify active handlers/connections return to baseline.

Send a small, fixed number of 1 MiB + 1 byte text, JSON, and multipart requests,
including chunked requests; expect 413 without sustained RSS/temp-file growth.
Keep an explicit request count and do not use an unbounded upload generator.

Only add profiling, writer pools, or further concurrency limits if measurements
identify a remaining bottleneck. Keep any profiling endpoint local and temporary.

## Fault and protocol fixtures

`/unstable`, `/weighted-status`, and `/retry-after` deliberately return failure
statuses. `/abort` deliberately truncates a response: a transfer error is expected,
not a successful complete download. Do not let automatic retries turn these into
an unbounded workload. Set a maximum retry count and total deadline on test clients.

`/large-headers` caps generated values at 64 KiB; cookie output at 16 KiB; checksum,
multipart echo, and download bodies at 1 MiB. Use small finite request counts when
testing client limits. Compression negotiation reuses precomputed fixture bytes.
Protocol tests in `pkg/server/resilience_test.go` exercise informational responses
and stream aborts over both HTTP/1.1 and HTTP/2, including connection reuse after an
HTTP/2 reset. Proxies may change which partial bytes or 1xx headers reach a client.
