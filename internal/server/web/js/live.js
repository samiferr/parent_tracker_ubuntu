import { useState, useEffect, useRef } from './h.js';
import { api } from './api.js';

// The live view (docs/04 §4.11, FR-6.8–6.11). It listens to a Server-Sent
// Events stream; if streaming fails it falls back to polling a snapshot every
// max(interval, 5 s). It never presents old numbers as current: when nothing has
// arrived for 3 sampling intervals the status becomes "stale".
//
// status: connecting | live | reconnecting | stale | off | waiting
//   off     — the live view is switched off on the server
//   waiting — connected, but the collectors have not reported yet
export function useLive(user, enabled) {
  const [s, setS] = useState({ status: enabled ? 'connecting' : 'off', snap: null, polling: false, lastOk: 0, skew: 0 });
  const refs = useRef({ lastOk: 0, interval: 5000 });

  useEffect(() => {
    if (!enabled || !user) { setS({ status: 'off', snap: null, polling: false, lastOk: 0, skew: 0 }); return undefined; }
    let closed = false;
    let es = null;
    let pollTimer = null;
    let failures = 0;
    refs.current = { lastOk: 0, interval: 5000 };
    setS({ status: 'connecting', snap: null, polling: false, lastOk: 0, skew: 0 });

    const accept = (snap, polling) => {
      if (closed || !snap || snap.available === false) return;
      refs.current.lastOk = Date.now();
      refs.current.interval = snap.interval_ms || 5000;
      failures = 0;
      setS({ status: 'live', snap, polling, lastOk: refs.current.lastOk, skew: (snap.server_time || Date.now()) - Date.now() });
    };

    const poll = async () => {
      try {
        const snap = await api.get('/api/live', { user });
        if (closed) return;
        if (snap.available === false) setS((p) => ({ ...p, status: 'waiting', polling: true }));
        else accept(snap, true);
      } catch (e) {
        if (closed) return;
        if (e.status === 404) { setS({ status: 'off', snap: null, polling: false, lastOk: 0, skew: 0 }); return; }
        failures++;
        setS((p) => ({ ...p, status: p.status === 'stale' ? 'stale' : 'reconnecting', polling: true }));
      }
      if (!closed) pollTimer = setTimeout(poll, Math.max(refs.current.interval, 5000));
    };

    const startPolling = () => {
      if (es) { es.close(); es = null; }
      if (!pollTimer) poll();
    };

    const open = () => {
      if (typeof EventSource === 'undefined') { startPolling(); return; }
      es = new EventSource('/api/live/stream?user=' + encodeURIComponent(user));
      const onSnap = (ev) => { try { accept(JSON.parse(ev.data), false); } catch (e) { /* ignore a bad event */ } };
      ['snapshot', 'state', 'tick'].forEach((n) => es.addEventListener(n, onSnap));
      es.addEventListener('unavailable', () => { if (!closed) setS((p) => ({ ...p, status: p.snap ? p.status : 'waiting' })); });
      es.addEventListener('bye', () => { if (!closed) { es.close(); setS({ status: 'off', snap: null, polling: false, lastOk: 0, skew: 0 }); } });
      es.onerror = () => {
        if (closed) return;
        failures++;
        setS((p) => ({ ...p, status: p.status === 'stale' ? 'stale' : 'reconnecting' }));
        // CLOSED means the browser gave up (an HTTP error such as 503 or 404, or a
        // blocked stream): use the snapshot endpoint instead. After repeated
        // failures do the same, since an intermediary may be buffering the stream.
        if (es.readyState === 2 || failures >= 3) startPolling();
      };
    };
    open();

    // staleness: nothing received for 3 sampling intervals (at least 15 s)
    const watchdog = setInterval(() => {
      const { lastOk, interval } = refs.current;
      if (!lastOk) return;
      if (Date.now() - lastOk > Math.max(3 * interval, 15000)) setS((p) => (p.status === 'live' || p.status === 'reconnecting' ? { ...p, status: 'stale' } : p));
    }, 1000);

    return () => { closed = true; clearInterval(watchdog); if (pollTimer) clearTimeout(pollTimer); if (es) es.close(); };
  }, [user, enabled]);

  return s;
}
