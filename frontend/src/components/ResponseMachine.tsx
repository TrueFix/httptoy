import { useState, type PointerEvent } from 'react';
import { AnimatePresence, motion, useMotionValue, useReducedMotion, useSpring, useTransform } from 'framer-motion';
import { ArrowDown, ArrowUpRight, RotateCcw } from 'lucide-react';

const statuses = [200, 404, 418, 503] as const;
const names: Record<number, string> = { 200: 'OK', 404: 'Not found', 418: "I'm a teapot", 503: 'Service unavailable' };
const spring = { type: 'spring', duration: 0.5, bounce: 0.2 } as const;

/** A real status-code fixture, art-directed as a physical arcade response ticket. */
export function ResponseMachine() {
  const [selected, setSelected] = useState<number>(503);
  const [response, setResponse] = useState<{ code: number; elapsed: string } | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(false);
  const [run, setRun] = useState(0);
  const reduce = useReducedMotion();
  const pitch = useMotionValue(0);
  const yaw = useMotionValue(0);
  const smoothPitch = useSpring(pitch, spring);
  const smoothYaw = useSpring(yaw, spring);
  const transform = useTransform([smoothPitch, smoothYaw], ([x, y]) => `rotateX(${x}deg) rotateY(${y}deg) rotateZ(-7deg)`);
  function follow(event: PointerEvent<HTMLDivElement>) {
    if (reduce || event.pointerType !== 'mouse' || !matchMedia('(hover: hover) and (pointer: fine)').matches) return;
    const bounds = event.currentTarget.getBoundingClientRect();
    yaw.set(((event.clientX - bounds.left) / bounds.width - 0.5) * 16);
    pitch.set(-((event.clientY - bounds.top) / bounds.height - 0.5) * 12);
  }
  async function fire() {
    if (busy) return;
    setBusy(true); setError(false);
    const started = performance.now();
    try {
      const result = await fetch(`/status/${selected}`, { cache: 'no-store', credentials: 'omit', signal: AbortSignal.timeout(5000) });
      await result.text();
      setResponse({ code: result.status, elapsed: `${(performance.now() - started).toFixed(1)} ms` });
      setRun(value => value + 1);
    } catch { setError(true); }
    finally { setBusy(false); }
  }
  const code = response?.code ?? selected;
  return <div className="response-machine" data-ui aria-label="Interactive HTTP response machine">
    <div className="machine-scene" onPointerMove={follow} onPointerLeave={() => { pitch.set(0); yaw.set(0); }}>
      <div className="ticket-stack" aria-hidden="true"><div className="ticket-back ticket-back-one"><span>HTTP / RESPONSE</span><strong>404</strong></div><div className="ticket-back ticket-back-two"><span>HTTP / RESPONSE</span><strong>200</strong></div></div>
      <motion.div className="response-ticket" style={{ transform: reduce ? 'none' : transform }}>
        <div className="ticket-header"><span>httptoy / response</span><ArrowUpRight size={25} aria-hidden="true" /></div>
        <div className="ticket-code-window"><AnimatePresence mode="popLayout" initial={false}><motion.strong key={`${code}-${run}`} className="ticket-code" initial={reduce ? { opacity: 0.6 } : { transform: 'translateY(75%) rotateX(-35deg)', opacity: 0 }} animate={{ transform: 'translateY(0%) rotateX(0deg)', opacity: 1 }} exit={reduce ? { opacity: 0 } : { transform: 'translateY(-75%) rotateX(35deg)', opacity: 0 }} transition={{ duration: 0.28, ease: [0.23, 1, 0.32, 1] }}>{code}</motion.strong></AnimatePresence></div>
        <div className="ticket-name"><span>{names[code] || 'HTTP response'}</span><span className="ticket-hole" aria-hidden="true" /></div>
        <div className="ticket-footer"><code>GET /status/{selected}</code><span>{response ? response.elapsed : 'Preview fixture'}</span></div>
      </motion.div>
      <span className="machine-stamp" aria-hidden="true">FAILURE<br />IS A FEATURE.</span>
    </div>
    <div className="machine-controls"><div className="machine-statuses" role="group" aria-label="Choose a response code">{statuses.map(status => <button key={status} type="button" aria-pressed={selected === status} disabled={busy} onClick={() => { setSelected(status); setResponse(null); setError(false); }}>{status}</button>)}</div><button className="machine-fire" type="button" disabled={busy} onClick={() => void fire()}>{busy ? 'In flight…' : response ? 'Fire again' : 'Fire a request'}{response ? <RotateCcw size={18} aria-hidden="true" /> : <ArrowUpRight size={20} aria-hidden="true" />}</button></div>
    <p className="machine-feedback" role="status">{error ? 'Connection interrupted. Give it another shot.' : response ? `Actual HTTP ${response.code} received. Your move.` : 'Pick a code. Get a real HTTP response.'}</p>
    <noscript><a href="/status/503" rel="nofollow">Open a real 503 response <ArrowDown size={16} /></a></noscript>
  </div>;
}
