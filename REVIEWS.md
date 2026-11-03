# Integrated review outcomes

Reviewed with the requested security-review, perf, bug-hunter, and dependency-audit
agents. This records the implemented remediations and remaining operational risks;
it is not a claim that a public testing service is safe without deployment controls.

## Security

- Fixed access-log injection through percent-encoded LF/CR/escape characters in
  session paths. Masked paths are escaped before logging. Query strings are omitted
  and common sensitive echo/authentication path fields are masked.
- Fixed the OpenAPI session sequence schema: its enum is generated from the same
  recognized final HTTP statuses accepted at runtime (299 is not accepted).
- Tests cover session isolation, expiry/deletion, token handling, sensitive-header
  redaction, authorization before body reading and reauthorization after reading.
- No verified high-severity issue was found in the documented test-fixture model.

Residual requirements: use TLS and trusted proxy configuration, transport deadlines,
network/connection admission controls, and test-only data. Anonymous clients can
exhaust all 64 session slots until TTL expiry. Header redaction is name-based;
bodies, URLs and unrecognized credential headers can contain secrets. Postman users
must clear saved token variables before sharing an exported collection. Handler-only
embedding does not configure the deadlines supplied by `server.HTTPServer`.

## Correctness

- Fixed regional language exclusions incorrectly rejecting an acceptable generic
  language (`en-US;q=0,en;q=1` now selects en). Numeric region tags such as es-419
  are accepted.
- Fixed XML declaration position, uniqueness and syntax checks, including declarations
  after a root and repeated/mis-cased declarations. UTF-8 BOM input is supported;
  validation is a bounded XML 1.0 parsing fixture, not DTD/business-schema validation.
- Fixed slash-containing Base64 path data, including `/base64de/%2Fw==`, without
  changing global routing normalization.
- Clarified and tested trailer limits: 4096 bytes total, 32 names, 32 values per name.
- Expect rejection validates its boolean flag and does not emit a connection-specific
  header on HTTP/2. Protocol tests cover actual HTTP/1.1 Continue acceptance/rejection.

## Performance

- Wire and expanded decompression limits are independent, including chunked bodies
  and concatenated gzip members. Eight work slots bound concurrent validation and
  decompression; stream/delay/WebSocket budgets remain separate.
- Session storage and key/record counts are bounded. Body reads, hashing, response
  encoding and network writes do not hold the store mutex. Slow-body and concurrent
  idempotency regressions pass under the race detector.
- Malformed bearer-token lengths are rejected before hashing.
- Discovery documents are built once and served from immutable cached bytes.

Agent measurements on an AMD Ryzen 7 5800XT, isolated protocol router with HTTP
recorders (mean values, not production p95): negotiation 3.59 µs / 4.38 KB allocated;
gzip → 1 MiB 0.676 ms / 3.33 MB; deflate → 1 MiB 0.912 ms / 3.33 MB; Brotli → 1 MiB
2.51 ms / 4.41 MB. Allocations are not peak live memory and exclude real networking.

Remaining: slow clients can occupy work slots; retained-session byte accounting is
not an RSS limit; concurrent capture-list responses allocate snapshots/serialized
responses. Measure full-server mixed workloads, p95/p99, RSS/GC, cold discovery and
503 rates before introducing pooling, sharding or more admission complexity.

## Dependencies and distribution

- Four direct production dependencies; no new dependencies were needed for the
  workflow/protocol/discovery additions. No newer direct releases or known reachable
  vulnerabilities were reported by the audit.
- `go mod verify` and `go mod tidy -diff` pass. Legitimate dependency-test checksums
  remain in go.sum; they are not linked into the application.
- Production builder is pinned to verified Go 1.26.10 image digest. CI uses that
  exact version, pinned action commits and govulncheck v1.9.0, verifies lockfile
  integrity/drift, and scans both source and the statically built binary. Weekly
  checks refresh advisory data; Dependabot proposes dependency/image/action updates.
- Dependency copyright/license notices are included with the scratch image alongside
  the project and Go licenses. Revisit the notice inventory when modules change.

Validation: Go 1.26.10 race/coverage tests, vet, module integrity/tidy checks, source
vulnerability scan and static binary scan. Container build execution still requires
a Docker daemon; one is not available in this workspace.
