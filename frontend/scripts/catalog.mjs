import { execFileSync } from 'node:child_process';
import { readFileSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('../../', import.meta.url));
const document = JSON.parse(process.argv[2] ? readFileSync(process.argv[2], 'utf8') : execFileSync('go', ['run', './cmd/site-data'], { cwd: root, encoding: 'utf8', maxBuffer: 8 * 1024 * 1024 }));
const skip = new Set(['/', '/assets/', '/favicon.ico', '/sitemap.xml']);
const groupFor = (path) => {
  if (path.startsWith('/sessions')) return 'Workflows';
  if (/^\/(anything|get|post|put|patch|delete|headers|header|ip|connection|expect-continue|request-trailers)/.test(path)) return 'Requests';
  if (/^\/(stream|sse|ws|drip|trailers|delay)/.test(path)) return 'Streams & timing';
  if (/^\/(basic|bearer|auth|cookies)/.test(path)) return 'Auth & cookies';
  if (/^\/(status|problem|unstable|retry-after|weighted-status|abort|early-hints|informational|large-headers|redirect|cache|etag|range)/.test(path)) return 'Responses & resilience';
  if (/^\/(image|text|json|xml|yaml|html|bytes|body|base64|utf8|gzip|zlib|brotli|deflate|encoding|compression|decompress|checksum|upload|multipart|validate|download|language|negotiate|links)/.test(path)) return 'Payloads & formats';
  return 'Tools & discovery';
};
const descriptions = {
  '/anything': 'Echo any supported HTTP method, headers, query parameters, and body.',
  '/anything/{path}': 'Inspect a request at any nested path, with configurable CORS.',
  '/get': 'Inspect query parameters, request headers, client IP, and URL.',
  '/post': 'Inspect a POST body as JSON, form data, multipart, or text.',
  '/put': 'Inspect a PUT request and its body.', '/patch': 'Inspect a PATCH request and its body.', '/delete': 'Inspect a DELETE request and its body.',
  '/headers': 'Return the incoming request headers.', '/header/{header_name}': 'Read a single request header.', '/ip': 'Return the proxy-validated client IP.',
  '/status/{code}': 'Return a recognized HTTP response status.', '/delay/{seconds}': 'Delay a response by up to ten seconds; honor cancellation.',
  '/stream/{count}': 'Stream newline-delimited JSON records.', '/stream-bytes/{count}': 'Stream random bytes in configurable chunks.',
  '/sse': 'Send a finite stream of Server-Sent Events.', '/ws': 'Echo text and binary messages over WebSocket.',
  '/cache/{seconds}': 'Set cache headers and exercise conditional requests.', '/etag/{value}': 'Test entity tags, 304 responses, and preconditions.',
  '/range/{size}': 'Test byte ranges, resumable downloads, and 206 responses.', '/redirect-to': 'Redirect to an HTTP(S) URL or local path without fetching it.',
  '/unstable': 'Randomly return 503 at a configurable failure rate.', '/retry-after/{seconds}': 'Return 429 or 503 with a Retry-After header.',
  '/weighted-status': 'Choose a response status from a weighted distribution.', '/abort': 'Deliberately interrupt a response to test client recovery.',
  '/metrics': 'Prometheus counters, latency histograms, and resource-pool gauges.', '/robots.txt': 'Crawler policy and sitemap location for this instance.',
};
const methods = new Set(['get', 'post', 'put', 'patch', 'delete', 'head', 'options', 'trace', 'x-connect']);
const endpoints = Object.entries(document.paths).filter(([path]) => !skip.has(path)).map(([path, operations]) => {
  const entries = Object.entries(operations).filter(([method]) => methods.has(method));
  const first = entries[0]?.[1] || {};
  const fallback = first.summary?.startsWith('HTTP test fixture:') ? `Test ${path.split('/')[1].replaceAll('-', ' ')} behavior with a bounded HTTP fixture.` : first.summary;
  return { path, methods: entries.map(([method]) => method === 'x-connect' ? 'CONNECT' : method.toUpperCase()), description: descriptions[path] || fallback, group: groupFor(path), auth: path.startsWith('/sessions/{id}') };
});
writeFileSync(new URL('../src/catalog.json', import.meta.url), JSON.stringify(endpoints, null, 2) + '\n');
