# httptoy reference

[← Back to the project](../README.md)

[Run locally](#run-locally) · [Development](#frontend-development) ·
[API endpoints](#api-endpoints) · [Workflow sandboxes](#workflow-sandboxes) ·
[Metrics](#prometheus-monitoring)

## Run locally

Build from this checkout with Go 1.26 or newer (templates and sample files are
embedded, so the binary can run from any directory):

```sh
go run .
# Or build the local source into an unprivileged container:
docker build -t httptoy .
docker run --rm -p 8000:8000 httptoy
```

### Limits and deployment

- Echo request bodies are limited to **1 MiB**, including chunked and multipart
  uploads. Oversized bodies return **413**, malformed supported bodies **400**.
  JSON must be an object; nested objects, arrays, numbers, booleans and null values
  are preserved. Form values remain semicolon-joined strings.
- Both delay endpoints cap each wait at **10 seconds** and stop waiting when a
  client disconnects. Negative delays and byte counts return **400**. At most
  **128 nonzero delays** may wait concurrently; excess waits return **503** with
  `Retry-After: 1`. Redirects are separate client-followed requests, not recursion.
- Server deadlines: 5s headers, 15s request reads, 20s response writes, 60s idle.
  SIGINT/SIGTERM allows up to 25s for graceful shutdown. When using a proxy, ensure
  its deadlines accommodate permitted delays.
- No proxies are trusted by default. Set `TRUSTED_PROXIES` to a comma-separated
  list of proxy IPs/CIDRs (for example `192.0.2.1,198.51.100.0/24`) only when those
  peers are controlled by you. Forwarded client IPs are ignored otherwise.
- Routing uses `github.com/gorilla/mux` with Go's built-in `net/http.Server`.
  Request logging and panic recovery remain enabled. Query strings are omitted
  from logs and common sensitive echo/authentication path values are masked.
  Never send real credentials to this testing
  service: authentication endpoints deliberately echo credentials and paths are
  logged. Deploy behind appropriate access controls/TLS.

### Compatibility fixes

`PATCH /patch` now works (`PATCH /delete` is no longer registered). JPEG endpoints
return the supplied JPEG rather than an empty response. Header inspection includes
`X-*` headers, and echoed URLs include escaped paths and query strings. Bearer
authentication requires a valid, nonempty token after the scheme; it does not
validate a token against an identity provider.

`/deflate` and `/zlib` both return zlib-wrapped data with HTTP
`Content-Encoding: deflate`. `/utf8` uses a UTF-8 charset, not a content encoding.
Compressed responses and embedded fixtures are prepared once, not regenerated
from disk on each request. Fixture edits require rebuilding.

### Frontend development

The homepage uses Vite, React, Tailwind CSS, and Framer Motion. Its complete API
catalog is pre-rendered and embedded with the assets in the Go binary. Checked-in
assets let `go run .` work without Node. After editing frontend sources or API
routes, rebuild before compiling Go:

```sh
cd frontend
npm ci
npm run typecheck
npm run build
npm audit
```

Use Node 24 (Docker pins 24.22.2). `npm run dev` starts Vite on localhost; run the
Go API separately with `go run .` on port 8000 for the dev proxy. The Docker build regenerates
the API catalog and frontend automatically, then ships only the Go binary.

Set `PUBLIC_URL=https://your-host.example` to your deployment's public origin
(default: `https://httptoy.com`). It must have no path, query, fragment, or
credentials. It controls canonical/social metadata, structured data, robots.txt,
and sitemap.xml; request Host headers are never used to generate canonical URLs.
API responses advertise `X-Robots-Tag: noindex, nofollow`; the homepage is fully
readable without JavaScript. This provides technical SEO, not guaranteed rankings.
No analytics or external font requests are included. Use synthetic data in demos.

Preserve [FRONTEND_NOTICES.md](../FRONTEND_NOTICES.md) with distributed binaries.
The embedded copy is served at `/assets/licenses.txt` and included in containers.

### Backend checks

```sh
go test -race ./...
go vet ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
go test ./pkg/routes -run '^$' -bench . -benchmem
```

Benchmarks use in-process HTTP recorders, not network traffic. See
[PERFORMANCE.md](../PERFORMANCE.md) for bounded load-testing guidance.

Docker pins Go 1.26.10 by image digest. CI extracts that exact toolchain version,
checks module integrity/tidy drift, scans source and the statically built binary,
and runs on a weekly schedule as well as pushes/PRs. Dependabot maintains module,
Docker and action pins; periodically review the pinned govulncheck version too.
When distributing a binary, include [THIRD_PARTY_NOTICES.md](../THIRD_PARTY_NOTICES.md),
this project's license and the Go license. The container includes them under
`/licenses`. Review findings and remaining deployment caveats are in
[REVIEWS.md](../REVIEWS.md).

## API Endpoints

Braces denote path parameters. Invalid delay and redirect integers default to 0;
invalid byte counts default to 1024. Byte responses are capped at 1024 bytes.
Status codes must be recognized HTTP status codes.

### Extended APIs

New endpoints reject out-of-range or malformed numeric parameters with **400**,
rather than silently clamping them. Counts may be zero unless noted otherwise.

| Method | Path | Behavior and options |
| :--- | :--- | :--- |
| ANY | `/anything` or `/anything/{path...}` | Echo method, path, URL, headers, query, parsed data/text, and lossless `body_base64`. Accepts GET, HEAD, POST, PUT, PATCH, DELETE, CONNECT, TRACE, OPTIONS. Bodies capped at 1 MiB. |
| OPTIONS | `/anything` or `/anything/{path...}` | Configurable CORS preflight; returns 204. See options below. |
| GET | `/response-headers` | Query keys become response headers, including repeated values. Maximum 32 names, 4 KiB/value, 16 KiB encoded query. Unsafe header names/values and framing, connection, trailer, and encoding overrides are rejected. |
| GET | `/redirect-to?url=...` | Return a redirect without fetching the destination. Accepts absolute HTTP(S) URLs or local absolute paths. Optional `status_code`: 301, 302 (default), 303, 307, 308. Credentials in URLs are rejected. |
| GET | `/stream/{count}` | Flush 0–100 newline-delimited JSON records with sequential `id` values. Maximum request URI length: 4096 bytes. |
| GET | `/stream-bytes/{count}` | Flush 0–1 MiB random bytes. `chunk_size`: 1–65536, default 4096. HTTP transport/proxies may combine chunks. |
| GET | `/cache/{seconds}` | `Cache-Control: public, max-age=...`, ETag, Last-Modified; supports conditional requests. Seconds: 0–86400. |
| GET | `/etag/{value}` | Strong quoted ETag with conditional GET/If-Match handling; returns 304 or 412 as appropriate. Value: up to 128 printable ASCII bytes, excluding quotes and spaces. |
| GET | `/range/{size}` | Deterministic repeating `a`–`z` bytes, size 0–1 MiB. Supports Range, suffix/multipart ranges, If-Range, 206 and 416. |
| GET | `/healthz` | Returns `{"status":"ok"}` without external dependencies. |
| GET | `/sse` | SSE `message` events with sequential IDs. `count`: 0–100 (default 5); `interval_ms`: 0–1000 (default 100). Total intentional wait ≤10s. |
| GET | `/ws` | WebSocket text/binary echo. Maximum 64 KiB/message, 100 messages, 30s connection lifetime, 64 simultaneous connections. Browser origins must match the request host; non-browser clients may omit Origin. |
| GET | `/drip` | Gradually send `*` bytes. `numbytes`: 1–1024 (default 10); `duration`: 0–10 whole seconds (default 1); `delay`: 0–10 seconds (default 0). Sum of delay + duration ≤10s. A one-byte response has no between-byte wait. |
| GET | `/trailers` | Text response followed by `X-Checksum` (SHA-256) and `X-Stream-Complete` trailers. Read the full response before inspecting trailers. |
| GET | `/links/{count}` | Generate 0–100 HTML links pointing to `/anything/links/{index}`. |
| GET | `/robots.txt` | Crawler policy with `Disallow: /deny` and the configured public sitemap URL. |
| POST | `/upload` | Multipart upload inspection: repeated fields, file field/name, byte size, sniffed MIME type, SHA-256. Total 1 MiB, at most 100 parts. Files are streamed into hashes and never saved. |
| GET | `/auth/digest` | HTTP Digest challenge/response using SHA-256, `qop=auth`, realm `httptoy`. Test credentials: **user / passwd**. Nonces expire after five minutes; repeated/non-increasing nonce counts are rejected. |

Streaming endpoints (`stream`, `stream-bytes`, `sse`, `sse/resume`, `drip`, `trailers`, `abort`) share a
128-request admission budget. Excess requests return **503** with `Retry-After: 1`.
Streams stop on cancellation and have a 12s deadline, allowing transport overhead
around at most 10s of deliberate waits. A deadline/write failure can truncate a
response already in progress. Reverse proxies may buffer output or discard trailers.

Digest replay state is process-local and bounded to 4096 active nonce/client-nonce
pairs; exhaustion returns 503. Use sticky routing when testing through multiple
instances. This endpoint is a protocol fixture, **not production authentication**.

#### CORS options

CORS controls apply only to `/anything`, including non-OPTIONS responses:

- `allow_origin`: default `*`; may be `null` or one explicit HTTP(S) origin.
- `allow_credentials`: boolean, default `false`; cannot be combined with `*`.
- `allow_methods`: comma-separated methods; defaults to GET, POST, PUT, PATCH,
  DELETE, HEAD, OPTIONS. This controls the CORS header, not registered routes.
- `allow_headers`: comma-separated header names; defaults to the requested
  `Access-Control-Request-Headers`.
- `max_age`: 0–86400 seconds, default 600.

Examples:

```sh
curl -X POST http://localhost:8000/anything/example -H 'Content-Type: application/json' -d '{"ok":true}'
curl -N http://localhost:8000/stream/3
curl -N 'http://localhost:8000/sse?count=3&interval_ms=500'
curl -i -H 'Range: bytes=2-5' http://localhost:8000/range/26
curl -i -H 'If-None-Match: "demo"' http://localhost:8000/etag/demo
curl -F 'attachment=@README.md' -F 'tag=example' http://localhost:8000/upload
curl --digest -u user:passwd http://localhost:8000/auth/digest
```


### Resilience and protocol APIs

These endpoints use the same Gorilla mux / `net/http` server. Invalid parameters
return **400**. Fault selection is stateless and independently randomized per
request; the generated status is intentional, not an application failure.

| Method | Path | Behavior and limits |
| :--- | :--- | :--- |
| GET | `/unstable?failure_rate=30` | Return 503 with the given percentage probability, otherwise 200. Integer rate 0–100, default 30. Responses are not cached. |
| GET | `/retry-after/{seconds}` | Return 429 by default, or `?status=503`. Seconds 0–86400. `format=seconds` (default) or `format=date` controls the Retry-After header; the server does not wait. |
| GET | `/weighted-status?codes=200:80,500:20` | Pick a recognized final status (200–599) using relative integer weights. Up to 16 pairs, each weight 1–10000, total ≤100000. Default 200:80,500:20. 204/205/304 have no body. |
| GET | `/abort?after_bytes=100` | Write 0–65536 `*` bytes, flush, then deliberately abort. Advertises one extra Content-Length byte to make truncation detectable. Uses the shared stream budget. |
| GET | `/early-hints` | Send 103 with `Link: </image/png>; rel=preload; as=image`, then a final 200 JSON response. Optional `link` replaces the hint (≤1024 bytes, no header injection). |
| GET | `/informational/{code}` | Send 100 or 102 before a final 200 JSON response. Other codes are rejected; use `/early-hints` for 103. |
| GET | `/large-headers?count=10&size=1024` | Generate `X-Test-Header-0`, etc., containing repeated `x` bytes. Count 0–64, size 0–4096; total generated values ≤64 KiB, excluding header names/framing. |
| GET | `/cookies/set?first=one&second=two` | Set multiple cookies with the options below. Returns 200 with set names, not a redirect. Existing `/cookies/set/{key}/{value}` behavior is unchanged. |
| GET | `/cookies/inspect` | Return raw Cookie header lines and an ordered parsed list, preserving duplicate cookie names. Never send real session cookies. |
| POST | `/multipart/response` | Echo a raw request body (≤1 MiB) as multipart/mixed: first a JSON `{bytes, sha256}` part, then an application/octet-stream attachment named `echo.bin`. No files are saved. |
| GET | `/encoding/{charset}` | Return `café déjà vu` plus newline in utf-8, utf-16 (big-endian with BOM), utf-16le, utf-16be, or iso-8859-1. LE/BE variants have no BOM. |
| GET | `/compression/negotiate` | Negotiate the existing fixture using Accept-Encoding. Supports br, gzip, deflate and identity; sets Vary: Accept-Encoding. Returns 406 when all supported representations are forbidden. |
| GET | `/connection` | Report actual HTTP version, socket peer address, proxy-validated client IP, and TLS version/cipher/ALPN/server name/handshake state, or `tls: null`. Reports the backend connection, not proxy TLS. |
| POST | `/checksum/{algorithm}` | Hash up to 1 MiB of raw request bytes using sha256 or sha512 (hyphenated aliases accepted). Returns `{algorithm, bytes, checksum}`; oversized/chunked inputs return 413. |
| GET | `/time` | Return a single server-time snapshot as `{rfc3339, unix, unix_ms}` in UTC, with Cache-Control: no-store. |
| GET | `/download/{filename}?size=1024` | Return deterministic repeating `a`–`z` bytes (0–1 MiB) with Content-Disposition. UTF-8 filenames use RFC 5987 extended parameters. `disposition=attachment` (default) or `inline`; filename must be a basename ≤255 UTF-8 bytes with no control characters. |

#### Configurable cookie options

On `/cookies/set`, all query keys other than the following become cookie names;
repeated values emit repeated Set-Cookie headers. All cookies are validated before
any are emitted: at most 16 cookies, 4 KiB per serialized cookie, 16 KiB total.
Query size is also limited to 16 KiB. Values must be valid HTTP cookie values (not
arbitrary binary or Unicode). Use the existing path-based setter for cookies whose
names conflict with reserved option keys.

| Option | Default | Values |
| :--- | :--- | :--- |
| `secure` | false | Boolean. |
| `httponly` | false | Boolean. |
| `samesite` | default | default, lax, strict, none; none requires secure=true. |
| `path` | / | Absolute cookie path. |
| `domain` | omitted | Valid cookie domain; browser domain restrictions still apply. |
| `max_age` | 0 (session) | -1 deletes; 1–31536000 gives lifetime in seconds. |
| `expires` | omitted | URL-encoded RFC 3339 timestamp. |

`__Secure-` names require Secure; `__Host-` names additionally require path=/ and
no Domain. Browsers enforce Secure/domain/path rules; receiving a Set-Cookie header
does not guarantee the browser accepts or sends that cookie.

#### Protocol expectations

- Informational responses are headers only. Use an HTTP trace/raw client to
  observe them; many clients display only the final response. Intermediaries may
  suppress 100/102/103.
- `/abort` intentionally causes a client read/request error. HTTP/1.1 closes the
  connection; HTTP/2 resets only that stream. Delivered partial bytes can vary,
  especially through HTTP/2 or buffering proxies. Do not expect a complete body or
  a final error-status response.
- Compression qvalues use RFC syntax (0–1, up to three decimal places), with up to
  32 entries/4096 header bytes. Explicit codings override `*`; identity defaults to
  quality 1 unless excluded. Equal-quality preference is br, gzip, deflate,
  identity. For example, gzip;q=0.5 alone selects identity; add identity;q=0 to
  require compression. Malformed negotiation headers return 400.
- Large-header and fault endpoints are bounded fixtures, not unbounded load-test
  generators. Test only clients and deployments you control.

```sh
curl -i 'http://localhost:8000/retry-after/30?status=503&format=date'
curl -i 'http://localhost:8000/weighted-status?codes=200:80,500:20'
curl -v 'http://localhost:8000/abort?after_bytes=100' # expected transfer error
curl -i http://localhost:8000/early-hints
curl --compressed -H 'Accept-Encoding: gzip, identity;q=0' http://localhost:8000/compression/negotiate
curl -i 'http://localhost:8000/cookies/set?demo=value&secure=true&httponly=true&samesite=lax'
curl --data-binary @README.md http://localhost:8000/checksum/sha256
curl -OJ 'http://localhost:8000/download/r%C3%A9sum%C3%A9.txt?size=128'
```

### Workflow sandboxes

`POST /sessions` creates an isolated, **process-local** sandbox. Save its random
`id` and `token`; every other session endpoint requires
`Authorization: Bearer <token>`. The token is returned only at creation and is not
stored in plaintext. Tokens are never accepted in URLs. Missing, wrong, deleted,
and expired credentials all return 401. Session responses use Cache-Control:
no-store. Restarting the process deletes all sessions; use sticky routing across
multiple instances.

Postman saves a created session's token in local collection variables. Clear those
variables before exporting or sharing a populated collection.

Creation accepts an optional JSON object (at most 8 KiB; unknown keys rejected):

```json
{
  "ttl_seconds": 300,
  "sequence": [503, 503, 200],
  "rate_limit": 5,
  "window_seconds": 60,
  "poll_after": 2
}
```

- TTL: 1–600 seconds; expired state is reclaimed on access and by a one-second
  cleanup worker. The maximum is **64 live sessions per process**; creation at
  capacity returns 503 with Retry-After.
- Sequence: 1–32 recognized final status codes (200–599).
- Rate limit: 1–1000 requests per 1–300 second fixed window.
- Polling: 0–100 pending responses before completion.

| Method | Path | Behavior |
| :--- | :--- | :--- |
| POST | `/sessions` | Return 201 with `{id, token, expires_at, config}`. |
| DELETE | `/sessions/{id}` | Immediately delete the sandbox; return 204. |
| POST | `/sessions/{id}/capture` | Capture one incoming webhook, returning 202 with record id/byte count. No outbound forwarding. Body ≤64 KiB, URL ≤4096 bytes, ≤32 header names/64 values/16 KiB. |
| GET | `/sessions/{id}/requests` | Return captured requests in arrival order, including method, URL, headers, timestamp and lossless body_base64. At most 32 records and 256 KiB accounted capture storage per session. Storage exhaustion returns 413. |
| ANY | `/sessions/{id}/sequence` | Return the next configured status, then repeat the final status indefinitely. Supports the same nine methods as `/anything`. 204/205/304 have no body. Request bodies are ignored. |
| POST | `/sessions/{id}/idempotency` | Require a 1–128 byte printable non-space ASCII Idempotency-Key. Hash up to 1 MiB of raw body plus Content-Type; identical repeats return the original 201/body with Idempotency-Replayed: true. A conflicting body/type returns 409. Up to 32 keys; exhaustion returns 429. |
| GET | `/sessions/{id}/rate-limit` | Consume the configured per-session allowance. Return 200 or 429, X-RateLimit-Limit/Remaining/Reset (reset is Unix seconds); denied requests include Retry-After seconds. |
| GET | `/sessions/{id}/poll` | Return 202/pending with Retry-After: 1 until poll_after is reached, then sticky 200/complete. No real background job is started. |

Capture redacts Authorization, Proxy-Authorization, Cookie, Set-Cookie, and headers
whose names contain key, token, or secret. **Bodies, URLs and other header values
may still contain secrets.** This is a testing fixture, not a production webhook
inbox or durable queue. Do not send real credentials or personal data. Use TLS and
access controls for remote deployment. Anonymous creation can fill the finite
session capacity; put shared/public deployments behind an appropriate admission
policy. The capture-byte accounting excludes Go object overhead and is not an RSS
limit.

```sh
# Save id/token from this response, or import /collection.json into Postman:
curl -X POST http://localhost:8000/sessions -H 'Content-Type: application/json' -d '{}'
curl -X POST -H "Authorization: Bearer $TOKEN" --data 'test webhook' "http://localhost:8000/sessions/$ID/capture"
curl -H "Authorization: Bearer $TOKEN" "http://localhost:8000/sessions/$ID/requests"
curl -H "Authorization: Bearer $TOKEN" "http://localhost:8000/sessions/$ID/sequence"
curl -X DELETE -H "Authorization: Bearer $TOKEN" "http://localhost:8000/sessions/$ID"
```

### Negotiation, validation and upload protocols

| Method | Path | Behavior and limits |
| :--- | :--- | :--- |
| GET | `/negotiate` | Honor Accept quality/specificity for application/json, application/xml, text/html, text/plain; absent header defaults to JSON. Return 406 if nothing is acceptable, 400 for malformed preferences. Vary: Accept. |
| GET | `/language` | Negotiate en, es, fr using Accept-Language, including regional fallback; absent header defaults to en. Return Content-Language and Vary: Accept-Language; 406 if excluded. |
| GET | `/problem/{status}` | Return a recognized 400–599 status as RFC 9457 application/problem+json with about:blank type, title, status and example detail. |
| GET | `/sse/resume` | Finite SSE stream, starting after Last-Event-ID (or at 0 without it). count: 0–100 (default 5), interval_ms: 0–1000 (default 100), total deliberate wait ≤10s; shared stream budget and cancellation. This is deterministic ID continuation, not persisted event replay. |
| POST | `/expect-continue` | Read ≤1 MiB and report byte count; net/http sends 100 Continue when it starts reading an expected upload. `reject=true` returns 417 without reading the request body; HTTP/1.1 rejection closes the connection. Known oversized Content-Length returns 413 before body reading. |
| POST | `/request-trailers` | Read ≤1 MiB fully, then return byte count and received trailer fields. Trailer values are available only after EOF; limits: 4096 bytes, 32 names, 32 values per name. |
| POST | `/decompress/{coding}` | Decode gzip, zlib-wrapped deflate, or br uploads. Both compressed and expanded bodies are independently capped at 1 MiB, including chunked data and concatenated gzip members. Return raw decoded bytes, 400 if malformed/checksum invalid, 413 on limits. |
| POST | `/validate/{format}` | Validate json (one value of any JSON type), xml (one root, nesting ≤128; DTD/directives unsupported), or multipart (Content-Type boundary ≤70 bytes, 1–100 parts). Body ≤1 MiB, no file retention; return validation metadata or a bounded parsing error. This does not validate business schemas or XML DTDs. |

Negotiation headers are limited to 4096 bytes/32 entries. Quality values follow the
same three-decimal syntax as compression negotiation. Specific matches override
wildcards, including explicit q=0 exclusions. Intermediaries can strip request
trailers, buffer SSE, or handle Expect on the client's behalf; test directly when
debugging protocol behavior.

Validation and decompression share an **8-request admission budget**, including
upload/read time; excess requests return 503 with Retry-After: 1. Codec windows and
parser bookkeeping can use more memory than the bounded body itself.

### Machine-readable discovery

- **GET `/openapi.json`**: OpenAPI 3.1 route inventory derived from the registered
  router, with path parameters, unique operation IDs, and session bearer security.
  Detailed query limits and fixture behaviors are documented here. Catch-all path
  parameters can span segments; CONNECT is represented as x-connect because
  OpenAPI has no CONNECT operation.
- **GET `/capabilities`**: runtime feature names, content codings, charsets,
  checksum algorithms, protocol/deployment requirements and resource limits.
- **GET `/collection.json`**: downloadable Postman v2.1 collection covering the
  routes, with base_url/session_id/session_token variables. Running POST /sessions
  populates the session variables. Binary compression/multipart and specialized
  protocol examples require matching request bodies/headers; consult this reference.

### Prometheus monitoring

**GET `/metrics`** exposes Prometheus text format 0.0.4 with HELP/TYPE declarations.
It uses the same HTTP listener, is unauthenticated by default, and should be
restricted to your monitoring network or protected by a reverse proxy. Metrics
contain aggregate operational data and build metadata, not captured bodies,
credentials, client addresses, session IDs, idempotency keys, or query strings.
No additional runtime dependencies are required.

```sh
curl http://localhost:8000/metrics
```

```yaml
scrape_configs:
  - job_name: httptoy
    scrape_interval: 15s
    static_configs:
      - targets: ['localhost:8000']
```

Use a reachable service hostname instead of localhost when Prometheus runs in a
different container or host.

| Metric | Type | Meaning |
| :--- | :--- | :--- |
| `httptoy_info{version,git_sha}` | gauge | Build metadata from APP_VERSION/GIT_SHA; defaults unknown/development. |
| `httptoy_uptime_seconds` | gauge | Process uptime. |
| `httptoy_http_requests_total{method,route,status}` | counter | Completed handlers by trusted route template and recorded status (default 200 if unset). |
| `httptoy_http_request_duration_seconds{method,route}` | histogram | Handler duration with cumulative buckets from 5 ms to 30s, plus +Inf/sum/count. |
| `httptoy_http_requests_in_flight` | gauge | Currently executing handlers, including the current scrape and upgraded connections. |
| `httptoy_http_aborted_requests_total` | counter | Explicit aborts, partial-response panic aborts, or canceled request contexts. |
| `httptoy_http_panics_total` | counter | Unexpected recovered panics; excludes deliberate ErrAbortHandler fixtures. |
| `httptoy_resource_pool_active{pool}` | gauge | Occupied slots in delays, streams, websockets, validation_decompression, sessions, metrics. |
| `httptoy_resource_pool_capacity{pool}` | gauge | Configured capacity of each pool. |
| `httptoy_admission_rejections_total{pool}` | counter | Capacity rejections since process/server start. |
| `httptoy_sessions_active` | gauge | Unexpired workflow sessions. |
| `httptoy_session_capture_bytes` | gauge | Accounted retained capture bytes, not total heap/RSS. |
| `httptoy_session_captured_requests` | gauge | Currently retained captured request records. |
| `httptoy_session_idempotency_keys` | gauge | Currently retained idempotency entries. |
| `httptoy_sse_clients` | gauge | Executing /sse and /sse/resume handlers. |

Labels are bounded: variable paths become templates such as
`/sessions/{id}/capture` and `/anything/{path}`. Unmatched paths (including
unsupported methods/trailing-slash redirects) use `route="unmatched"`; unknown
HTTP methods become OTHER and unrecognized status codes become other. Per-route
counter/histogram series appear on first completion, not as a large preallocated
set of zero series. Build-label values are bounded and escaped.

Request counters/histograms belong to the constructed server handler (normally
one per process); pools/session gauges describe shared process-wide state. All
counters reset on restart. Streams/WebSockets remain in flight until their
handlers finish. Deliberate delays and streaming lifetimes contribute to duration;
intentional failures contribute to their status counters. An aborted response can
still have committed status 200, so consult the separate aborted counter. A
scrape's own completion is visible on the next scrape. Two simultaneous metrics
scrapes are allowed; excess scrapes return 503 with Retry-After: 1.

Example PromQL for `/get` p95 duration in seconds:

```promql
histogram_quantile(0.95,
  sum by (le) (rate(httptoy_http_request_duration_seconds_bucket{route="/get"}[5m]))
)
```

### Methods

```http
GET /get
POST /post
DELETE /delete
PATCH /patch
PUT /put
```

### Auth

```http
GET /basic/{username}/{password}
GET /basic
GET /bearer
```

| Parameter | Type | Description |
| :--- | :--- | :--- |
| `username` | `string` | **Required** Username for auth |
| `password` | `string` | **Required** Password for auth |


### Compression

```http
GET /deflate
GET /brotli
GET /zlib
GET /gzip
GET /utf8
```

### Cookies

```http
GET /cookies
GET /cookies/set/{key}/{value}
GET /cookies/delete/{key}
```

| Parameter | Type | Description |
| :--- | :--- | :--- |
| `key` | `string` | **Required** Cookie name |
| `value` | `string` | **Required** Cookie value |

### Images

```http
GET /image
GET /image/png
GET /image/jpeg
GET /image/svg
GET /image/webp
```

### Redirect

```http
GET /redirect/{times}
GET /redirect-delay/{delay}/{times}
```

| Parameter | Type | Description |
| :--- | :--- | :--- |
| `times` | `int` | **Required** Number of time redirect  |
| `delay` | `int` | **Required** Delay in redirect in seconds  |

### Request Inspection

```http
GET /headers
GET /header/{header_name}
GET /ip
```

| Parameter | Type | Description |
| :--- | :--- | :--- |
| `header_name` | `string` | **Required** Name of the request header |


### Response

```http
GET /bytes/{count}
GET /base64en/{data}
GET /base64de/{data}

GET /delay/{seconds}
POST /delay/{seconds}
DELETE /delay/{seconds}
PUT /delay/{seconds}
PATCH /delay/{seconds}

GET /uuid
GET /body/{data}
GET /text
GET /json
GET /xml
GET /yaml
GET /html

```

| Parameter | Type | Description |
| :--- | :--- | :--- |
| `count` | `int` | **Required** Number of random bytes return  |
| `data` | `string` | **Required** String data  |
| `seconds` | `int` | **Required** Duration in seconds  |



### Response Status

```http
GET /status/{code}
POST /status/{code}
DELETE /status/{code}
PUT /status/{code}
PATCH /status/{code}
```

| Parameter | Type | Description |
| :--- | :--- | :--- |
| `code` | `int` | **Required** Valid HTTP [Status Codes](https://www.rfc-editor.org/rfc/rfc7231#section-6.1 "Status Codes") |
