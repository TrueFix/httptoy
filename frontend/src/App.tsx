import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { AnimatePresence, motion, useAnimationControls, useReducedMotion } from 'framer-motion';
import { ArrowDown, ArrowRight, ArrowUpRight, Check, ChevronDown, Copy, Play, Search } from 'lucide-react';
import endpoints from './catalog.json';
import { ClosingCTA, Footer } from './components/kit';
import { ResponseMachine } from './components/ResponseMachine';

const repo = 'https://github.com/TrueFix/httptoy';
const ease = [0.23, 1, 0.32, 1] as const;
type Endpoint = { path: string; methods: string[]; description: string; group: string; auth: boolean };
const catalog = endpoints as Endpoint[];

export const questions = [
  ['What is httptoy?', 'httptoy is an open-source HTTP testing API. It provides request inspection, response fixtures, fault simulation, streaming, authentication examples, and short-lived workflow sandboxes for testing HTTP clients.'],
  ['Is httptoy an alternative to httpbin?', 'Yes. httptoy covers common httpbin-style use cases such as request echoes, status codes, redirects, cookies, and compression, plus bounded workflow sessions, idempotency fixtures, and Prometheus metrics. Check the API reference when adapting existing tests.'],
  ['Do I need an API key?', 'Stateless test endpoints do not require an API key. Workflow sandboxes require the unique bearer token returned when you create a session. Authentication endpoints are protocol fixtures, not production identity services.'],
  ['Can I run it on my own machine?', 'Yes. Clone the repository and run the Go server, or build the included Docker image. The frontend and its assets are embedded in the binary; no frontend server is needed in production.'],
  ['What are the limits?', 'Echo bodies and compressed/expanded upload data are capped at 1 MiB. Delays are capped at 10 seconds. Sessions expire within 10 minutes, have bounded capture storage, and share finite process-wide admission limits. The capabilities endpoint and README describe each limit.'],
  ['Is it safe to send production data?', 'No. Use synthetic test data. Common sensitive capture headers are redacted, but URLs, request bodies, and other values can still contain secrets. For remote deployment, use TLS, access controls, and monitoring-network restrictions.'],
] as const;

function Reveal({ children, className = '', delay = 0 }: { children: ReactNode; className?: string; delay?: number }) {
  const controls = useAnimationControls();
  const reduce = useReducedMotion();
  const seen = useRef(false);
  return <motion.div className={className} initial={false} animate={controls} viewport={{ once: true, amount: 0.12 }} onViewportEnter={() => {
    if (seen.current || reduce) return;
    seen.current = true;
    void controls.start({ opacity: [0.25, 1], transform: ['translateY(20px)', 'translateY(0px)'], transition: { duration: 0.65, ease, delay } });
  }}>{children}</motion.div>;
}

function CopyButton({ text, requestPath, label = 'Copy command' }: { text: string; requestPath?: string; label?: string }) {
  const [copied, setCopied] = useState(false);
  const [error, setError] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);
  return <button className="copy-button" type="button" aria-label={copied ? 'Copied' : label} onClick={async () => {
    try { await navigator.clipboard.writeText(requestPath ? `curl '${new URL(requestPath, location.origin).href}'` : text); setCopied(true); setError(false); clearTimeout(timer.current); timer.current = setTimeout(() => setCopied(false), 1800); }
    catch { setError(true); }
  }}>{copied ? <Check size={17} aria-hidden="true" /> : <Copy size={17} aria-hidden="true" />}<span>{error ? 'Select to copy' : copied ? 'Copied' : 'Copy'}</span></button>;
}

function Header() {
  return <header className="site-header"><nav className="site-frame navigation" aria-label="Main navigation">
    <a className="wordmark" href="#top" aria-label="httptoy home"><img src="/assets/favicon.svg" alt="" width="34" height="34" />httptoy</a>
    <div className="nav-links"><a href="#playground">Playground</a><a href="#api-reference">API reference</a><a href="#self-host">Self-host</a></div>
    <a className="repository-link" href={repo}>GitHub <ArrowUpRight size={17} aria-hidden="true" /></a>
  </nav></header>;
}

type DemoResponse = { status: number; elapsed: string | null; body: string; kind: 'example' | 'live'; contentType: string };
const initialResponse: DemoResponse = { status: 200, elapsed: null, body: '{\n  "args": {\n    "message": "hello, httptoy"\n  },\n  "method": "GET",\n  "path": "/anything/hello"\n}', kind: 'example', contentType: 'application/json' };

async function request(path: string, options: RequestInit = {}) {
  const response = await fetch(path, { ...options, signal: options.signal ?? AbortSignal.timeout(5000), cache: 'no-store', credentials: 'omit' });
  return response;
}

function RequestPlayground() {
  const [preset, setPreset] = useState('echo');
  const [message, setMessage] = useState('hello, httptoy');
  const [result, setResult] = useState<DemoResponse>(initialResponse);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const reduce = useReducedMotion();
  const path = preset === 'echo' ? `/anything/hello?message=${encodeURIComponent(message)}` : preset === 'status' ? '/status/418' : '/json';
  const command = `curl '${path}'`;
  async function send() {
    if (loading) return;
    setLoading(true); setError('');
    const started = performance.now();
    try {
      const response = await request(path);
      let body = await response.text();
      try { body = JSON.stringify(JSON.parse(body), null, 2); } catch { /* Text and intentionally empty responses are valid. */ }
      setResult({ status: response.status, elapsed: `${(performance.now() - started).toFixed(1)} ms`, body: body.slice(0, 24000) || '(empty response body)', kind: 'live', contentType: response.headers.get('Content-Type')?.split(';')[0] || 'no content type' });
    } catch { setError('The request could not finish. Check your connection or try again.'); }
    finally { setLoading(false); }
  }
  return <div className="request-console" data-ui id="playground" aria-label="Live HTTP request playground">
    <div className="console-top"><span className="console-title">Your HTTP playground</span><span className="console-origin">Same-origin requests</span></div>
    <form onSubmit={event => { event.preventDefault(); void send(); }}>
      <div className="preset-row"><label htmlFor="request-preset">Try a request</label><div className="select-wrap"><select id="request-preset" value={preset} onChange={event => setPreset(event.target.value)}><option value="echo">Echo a request</option><option value="status">Return a 418</option><option value="json">Get sample JSON</option></select><ChevronDown size={15} aria-hidden="true" /></div></div>
      <div className="request-address"><span>GET</span><code>{preset === 'echo' ? '/anything/hello' : path}</code><span className="protocol">HTTP</span></div>
      {preset === 'echo' && <div className="message-field"><label htmlFor="echo-message">Message</label><input id="echo-message" maxLength={120} value={message} autoComplete="off" autoCapitalize="none" spellCheck={false} onChange={event => setMessage(event.target.value)} /></div>}
      <div className="send-row"><span>{preset === 'echo' ? 'See your message come back.' : preset === 'status' ? 'A deliberate failure. A successful test.' : 'A small, real JSON fixture.'}</span><button type="submit" className="send-button" disabled={loading}>{loading ? 'Sending…' : 'Send request'}<ArrowUpRight size={17} aria-hidden="true" /></button></div>
    </form>
    <div className="packet-track" aria-hidden="true"><span /><AnimatePresence>{loading && !reduce && <motion.i initial={{ transform: 'translateX(-20px)', opacity: 0 }} animate={{ transform: 'translateX(460px)', opacity: [0, 1, 1, 0] }} exit={{ opacity: 0 }} transition={{ duration: 0.8, ease: 'linear', repeat: Infinity }} />}</AnimatePresence></div>
    <div className="response-heading"><span>Response</span><div><span className={`response-code ${result.status >= 400 ? 'response-code-error' : ''}`}>{result.status} {result.status === 200 ? 'OK' : result.status === 418 ? "I'm a teapot" : ''}</span><span className="response-time">{loading ? 'In flight' : result.elapsed || 'Example output'}</span></div></div>
    <div className="response-body" aria-live="polite" aria-busy={loading}><pre tabIndex={0}><code>{error || result.body}</code></pre></div>
    <div className="console-bottom"><span>{error ? 'Request interrupted' : result.kind === 'example' ? 'Example excerpt · press Send for the real response' : result.contentType}</span><CopyButton text={command} requestPath={path} /></div>
    <noscript><p className="console-noscript">The playground needs JavaScript. <a href="/anything/hello?message=hello" rel="nofollow">Open a real JSON response instead.</a></p></noscript>
  </div>;
}

function FaultLab() {
  const [status, setStatus] = useState(503);
  const [result, setResult] = useState<number | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(false);
  const reduce = useReducedMotion();
  return <div className="fault-lab" data-ui>
    <div className="fragment-header"><span>Response laboratory</span><span>Real status codes</span></div>
    <div className="status-options" aria-label="Choose a status code">{[200, 404, 429, 503].map(code => <button type="button" key={code} aria-pressed={status === code} className={status === code ? 'selected' : ''} onClick={() => setStatus(code)}>{code}</button>)}</div>
    <div className="fault-result"><code>GET /status/{status}</code><AnimatePresence mode="wait" initial={false}><motion.strong key={result ?? 'ready'} initial={reduce ? false : { opacity: 0, transform: 'translateY(8px)' }} animate={{ opacity: 1, transform: 'translateY(0px)' }} exit={{ opacity: 0 }} transition={{ duration: 0.18, ease }}>{result ?? '—'}</motion.strong></AnimatePresence><span>{error ? 'Connection interrupted. Try again.' : result === null ? 'Choose a code. Make it happen.' : 'Actual HTTP response received.'}</span></div>
    <button className="dark-action" disabled={busy} onClick={async () => { setBusy(true); setError(false); try { const response = await request(`/status/${status}`); await response.text(); setResult(response.status); } catch { setError(true); } finally { setBusy(false); } }}>{busy ? 'Requesting…' : 'Try this response'}<ArrowRight size={18} aria-hidden="true" /></button>
  </div>;
}

function WorkflowLab() {
  const [results, setResults] = useState<number[]>([]);
  const [busy, setBusy] = useState(false);
  const [label, setLabel] = useState('Example sequence · 503 → 503 → 200');
  const reduce = useReducedMotion();
  async function run() {
    setBusy(true); setResults([]); setLabel('Creating a temporary sandbox…');
    let session: { id: string; token: string } | undefined;
    let completed = false;
    try {
      const response = await request('/sessions', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ttl_seconds: 30, sequence: [503, 503, 200] }) });
      if (response.status !== 201) throw new Error('Sandbox capacity unavailable.');
      session = await response.json();
      if (!session) throw new Error('Sandbox unavailable.');
      const values: number[] = [];
      for (let i = 0; i < 3; i++) {
        const result = await request(`/sessions/${encodeURIComponent(session.id)}/sequence`, { headers: { Authorization: `Bearer ${session.token}` } });
        await result.text(); values.push(result.status); setResults([...values]);
      }
      setLabel('Real responses received. Cleaning up…');
      completed = true;
    } catch { setLabel('The sandbox could not finish. Try again shortly.'); }
    finally {
      if (session) {
        try { const deleted = await request(`/sessions/${encodeURIComponent(session.id)}`, { method: 'DELETE', headers: { Authorization: `Bearer ${session.token}` } }); if (deleted.status === 204) setLabel(completed ? 'Run complete · sandbox deleted' : 'Run interrupted · sandbox deleted. Try again.'); else setLabel('Sandbox expires automatically within 30 seconds.'); }
        catch { setLabel('Sandbox expires automatically within 30 seconds.'); }
      }
      setBusy(false);
    }
  }
  return <div className="workflow-lab" data-ui>
    <div className="fragment-header"><span>One session. Three requests.</span><span>Isolated by token</span></div>
    <div className="sequence-output">{[503, 503, 200].map((example, index) => <div key={index}><span>Request {index + 1}</span><motion.strong initial={false} animate={{ opacity: results[index] ? 1 : 0.5, transform: results[index] && !reduce ? ['translateY(6px)', 'translateY(0px)'] : 'translateY(0px)' }} transition={{ duration: 0.25, ease }}>{results[index] ?? example}</motion.strong><span>{(results[index] ?? example) === 200 ? 'Recovered' : 'Try again'}</span></div>)}</div>
    <p aria-live="polite">{label}</p><button className="dark-action" disabled={busy} onClick={() => void run()}>{busy ? 'Running the sequence…' : 'Run this workflow'}<Play size={16} aria-hidden="true" /></button>
  </div>;
}

function MetricsFragment() {
  const [text, setText] = useState('httptoy_http_requests_total\nhttptoy_http_request_duration_seconds\nhttptoy_http_requests_in_flight\nhttptoy_sessions_active\nhttptoy_admission_rejections_total');
  const [label, setLabel] = useState('Available metric families');
  const [busy, setBusy] = useState(false);
  return <div className="metrics-fragment" data-ui><div className="fragment-header"><span>Prometheus, ready.</span><span>/metrics</span></div><pre tabIndex={0}><code>{text}</code></pre><div className="metrics-bottom"><span aria-live="polite">{label}</span><button disabled={busy} onClick={async () => { setBusy(true); try { const response = await request('/metrics'); if (!response.ok) throw new Error(); const body = await response.text(); setText(body.split('\n').filter(line => line && !line.startsWith('#') && /^(httptoy_uptime_seconds |httptoy_http_requests_in_flight |httptoy_sessions_active |httptoy_sse_clients |httptoy_http_panics_total )/.test(line)).slice(0, 5).join('\n')); setLabel('Live values from this instance'); } catch { setLabel('Metrics unavailable. Try again shortly.'); } finally { setBusy(false); } }}>{busy ? 'Fetching…' : 'Fetch live metrics'}<ArrowUpRight size={15} aria-hidden="true" /></button></div></div>;
}

const groupOrder = ['Requests', 'Responses & resilience', 'Streams & timing', 'Auth & cookies', 'Workflows', 'Payloads & formats', 'Tools & discovery'];

function CatalogGroup({ name, items, searching }: { name: string; items: Endpoint[]; searching: boolean }) {
  const [open, setOpen] = useState(name === 'Requests');
  return <details className="catalog-group" open={searching || open} onToggle={event => { if (!searching) setOpen(event.currentTarget.open); }}><summary><h3>{name}</h3><span>{items.length} paths<ChevronDown size={20} aria-hidden="true" /></span></summary><div className="endpoint-list">{items.map(item => <article className="endpoint" key={item.path}><div className="endpoint-path"><span className={`method method-${item.methods[0]?.toLowerCase()}`}>{item.methods.length > 5 ? 'ANY' : item.methods.join(' · ')}</span><code>{item.path}</code>{item.auth && <span className="auth-note">Bearer token</span>}</div><p>{item.description}</p></article>)}</div></details>;
}

function APICatalog() {
  const [query, setQuery] = useState('');
  const [method, setMethod] = useState('All');
  const filtered = useMemo(() => catalog.filter(item => (method === 'All' || item.methods.includes(method)) && `${item.path} ${item.description} ${item.group}`.toLowerCase().includes(query.toLowerCase().trim())), [query, method]);
  return <section className="catalog-section site-frame section-space" id="api-reference" aria-labelledby="api-title">
    <Reveal className="section-intro"><h2 id="api-title">Find your next test.</h2><p>{catalog.length} API paths. One place to explore request inspection, failure simulation, authentication, streaming, and the less convenient corners of HTTP.</p></Reveal>
    <div className="catalog"><div className="catalog-toolbar"><label className="search-field"><Search size={20} aria-hidden="true" /><span className="sr-only">Search API endpoints</span><input type="search" placeholder="Search endpoints, status codes, workflows…" value={query} onChange={event => setQuery(event.target.value)} /></label><label className="method-select"><span>Method</span><select value={method} onChange={event => setMethod(event.target.value)}>{['All', 'GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS'].map(value => <option key={value}>{value}</option>)}</select></label></div>
      <p className="catalog-result" role="status">{filtered.length} {filtered.length === 1 ? 'path' : 'paths'}{query || method !== 'All' ? ' matching your filters' : ' ready to explore'}</p>
      {groupOrder.map(name => { const items = filtered.filter(item => item.group === name); return items.length ? <CatalogGroup key={name} name={name} items={items} searching={Boolean(query) || method !== 'All'} /> : null; })}
      {!filtered.length && <div className="empty-search"><h3>No matching endpoints.</h3><p>Try “cookies”, “stream”, “retry”, or clear your filters.</p><button onClick={() => { setQuery(''); setMethod('All'); }}>Clear filters <ArrowRight size={17} aria-hidden="true" /></button></div>}
    </div>
    <div className="reference-links"><p>Need the exact parameters and limits?</p><a href={`${repo}/blob/main/README.md`}>Read the full reference <ArrowUpRight size={17} aria-hidden="true" /></a><a href="/openapi.json">OpenAPI <ArrowUpRight size={17} aria-hidden="true" /></a><a href="/collection.json">Postman collection <ArrowUpRight size={17} aria-hidden="true" /></a></div>
  </section>;
}

function SelfHost() {
  const [mode, setMode] = useState<'go' | 'docker'>('go');
  const command = mode === 'go' ? 'git clone https://github.com/TrueFix/httptoy.git\ncd httptoy\ngo run .' : 'git clone https://github.com/TrueFix/httptoy.git\ncd httptoy\ndocker build -t httptoy .\ndocker run --rm -p 8000:8000 httptoy';
  return <section className="self-host section-space" id="self-host" aria-labelledby="self-host-title"><div className="site-frame self-host-layout"><Reveal><h2 id="self-host-title">Your tests.<br />Your machine.</h2><p>The same HTTP playground, on your terms. One Go binary with the frontend embedded. No frontend server in production. No third-party request forwarding.</p><a href={`${repo}/blob/main/README.md#run-locally`} className="text-action">Deployment instructions <ArrowUpRight size={18} aria-hidden="true" /></a><p className="deployment-note">Go 1.26+ · Linux, macOS, Windows<br />Use a current patched toolchain. For remote deployments, configure TLS, PUBLIC_URL, trusted proxies, and access controls.</p></Reveal><Reveal delay={0.08}><div className="install-console" data-ui><div className="install-heading"><div className="install-options" aria-label="Installation method"><button aria-pressed={mode === 'go'} onClick={() => setMode('go')}>Go</button><button aria-pressed={mode === 'docker'} onClick={() => setMode('docker')}>Docker</button></div><CopyButton text={command} /></div><pre tabIndex={0}><code>{command}</code></pre><div className="install-foot"><span>Then open</span><code>http://localhost:8000</code><ArrowUpRight size={16} aria-hidden="true" /></div></div></Reveal></div></section>;
}

export default function App() {
  return <><a className="skip-link" href="#main">Skip to content</a><Header /><main id="main">
    <section className="hero site-frame" id="top" aria-labelledby="hero-title">
      <h1 id="hero-title"><span className="headline-mask"><span>BREAK THE </span></span><span className="headline-mask"><span>HAPPY PATH.</span></span></h1>
      <div className="hero-bottom"><div className="hero-copy"><p className="hero-description">An open-source HTTP testing API for the beautifully broken side of the web. Requests. Retries. Streams. All yours to play with.</p><a className="hero-explore" href="#playground">Enter the playground <ArrowDown size={19} aria-hidden="true" /></a></div><ResponseMachine /><div className="hero-aside"><p>Good clients<br />handle bad days.</p><span>Give yours a few.</span><a href="#api-reference">Explore {catalog.length} API paths <ArrowUpRight size={18} aria-hidden="true" /></a><p className="hero-footnote">Open source. MIT licensed.<br />No key. No sign-up. Just HTTP.</p></div></div>
    </section>
    <div className="compatibility site-frame"><p>Works wherever HTTP does.</p><div><span>curl</span><span>fetch()</span><span>Postman</span><span>Your test suite</span></div></div>
    <section className="request-section section-space" aria-labelledby="request-title"><div className="site-frame"><Reveal className="section-intro"><h2 id="request-title">Send it.<br />See everything.</h2><p>Your headers, your payload, your response. Send a real request and look under the hood. No mocks. No account. No mystery.</p></Reveal><Reveal><RequestPlayground /></Reveal></div></section>
    <section className="features site-frame section-space" id="features" aria-labelledby="features-title">
      <Reveal className="section-intro"><h2 id="features-title">HTTP, beyond 200 OK.</h2><p>The happy path is only the beginning. Give your client the responses it needs to handle before your users find them.</p></Reveal>
      <div className="feature-row"><Reveal className="feature-copy"><h3>Break it.<br />On purpose.</h3><p>Rate limits happen. Connections drop. Servers say no. Return the status you need, add a Retry-After header, or cut a response short to put recovery logic through a real test.</p><div className="endpoint-snippets"><code>/unstable</code><code>/retry-after/30</code><code>/abort</code></div><a className="text-action" href="#api-reference">Explore resilience fixtures <ArrowUpRight size={18} aria-hidden="true" /></a></Reveal><Reveal delay={0.08}><FaultLab /></Reveal></div>
      <div className="feature-row feature-row-reverse"><Reveal className="feature-copy"><h3>Test the whole<br />conversation.</h3><p>A webhook rarely ends with one request. Create a private, short-lived session to capture calls, test idempotency, simulate rate limits, and exercise a predictable retry sequence.</p><p>Sessions are isolated by bearer token and expire within ten minutes. This demo deletes its session when the run finishes.</p><a className="text-action" href={`${repo}/blob/main/README.md#workflow-sandboxes`}>Explore workflow sandboxes <ArrowUpRight size={18} aria-hidden="true" /></a></Reveal><Reveal delay={0.08}><WorkflowLab /></Reveal></div>
    </section>
    <section className="observability-section" aria-labelledby="observability-title"><div className="site-frame observability-layout"><Reveal><h2 id="observability-title">See what<br />your tests do.</h2><p>Streams, active sessions, request counts, and latency histograms. Prometheus metrics use bounded route templates—not client addresses, tokens, or session IDs.</p><a className="text-action" href="/metrics" rel="nofollow">Inspect the metrics endpoint <ArrowUpRight size={18} aria-hidden="true" /></a></Reveal><Reveal delay={0.08}><MetricsFragment /></Reveal></div></section>
    <APICatalog /><SelfHost />
    <section className="faq site-frame section-space" id="questions" aria-labelledby="questions-title"><Reveal className="faq-heading"><h2 id="questions-title">Read the fine print.</h2><p>No surprises. A few things to know before you start breaking things.</p></Reveal><div className="faq-list">{questions.map(([question, answer]) => <article className="faq-band" key={question}><h3>{question}</h3><p>{answer}</p></article>)}</div></section>
    <ClosingCTA /></main><Footer /></>;
}
