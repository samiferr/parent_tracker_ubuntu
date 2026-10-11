import { React, useState, useEffect, useRef, useCallback } from './h.js';
import { api } from './api.js';

// useFetch loads JSON whenever its inputs change and keeps the previous value
// on screen while the next one loads, so charts do not flicker.
export function useFetch(path, params, { every, enabled = true, refreshKey } = {}) {
  const [state, setState] = useState({ data: null, error: null, loading: enabled });
  const key = path + '?' + JSON.stringify(params || {}) + '#' + (refreshKey || '');
  const keyRef = useRef(key);
  keyRef.current = key;

  const load = useCallback(() => {
    if (!enabled) return () => {};
    const ctl = new AbortController();
    api.get(path, params, ctl.signal)
      .then((data) => { if (keyRef.current === key) setState({ data, error: null, loading: false }); })
      .catch((error) => {
        if (error.name === 'AbortError') return;
        if (keyRef.current === key) setState((s) => ({ data: s.data, error, loading: false }));
      });
    return () => ctl.abort();
  }, [key, enabled]); // eslint-disable-line

  useEffect(() => {
    setState((s) => ({ ...s, loading: true }));
    const cancel = load();
    let timer;
    if (every && enabled) timer = setInterval(load, every);
    return () => { cancel(); if (timer) clearInterval(timer); };
  }, [load, every, enabled]);

  return { ...state, reload: load };
}

// useMedia tracks a CSS media query.
export function useMedia(query) {
  const get = () => (window.matchMedia ? window.matchMedia(query).matches : false);
  const [match, setMatch] = useState(get);
  useEffect(() => {
    if (!window.matchMedia) return undefined;
    const mq = window.matchMedia(query);
    const on = () => setMatch(mq.matches);
    mq.addEventListener('change', on);
    on();
    return () => mq.removeEventListener('change', on);
  }, [query]);
  return match;
}

export const useReducedMotion = () => useMedia('(prefers-reduced-motion: reduce)');
// Lumen implements the desktop scale only; on narrow screens we use its larger
// control sizes (DS-12).
export const useNarrow = () => useMedia('(max-width: 40rem)');

// useNow re-renders every `ms`, unless the person prefers reduced motion: then
// counters move only when real data arrives (DS-7).
export function useNow(ms, active = true) {
  const reduced = useReducedMotion();
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    if (!active || reduced) return undefined;
    const t = setInterval(() => setNow(Date.now()), ms);
    return () => clearInterval(t);
  }, [ms, active, reduced]);
  return now;
}

export function useTheme() {
  const read = () => { try { return localStorage.getItem('pt-theme') || 'system'; } catch (e) { return 'system'; } };
  const [choice, setChoice] = useState(read);
  const dark = useMedia('(prefers-color-scheme: dark)');
  useEffect(() => {
    const theme = choice === 'light' || choice === 'dark' ? choice : (dark ? 'dark' : 'light');
    document.documentElement.setAttribute('data-theme', theme);
    document.documentElement.style.colorScheme = theme;
  }, [choice, dark]);
  const set = useCallback((c) => {
    try { if (c === 'system') localStorage.removeItem('pt-theme'); else localStorage.setItem('pt-theme', c); } catch (e) { /* ignore */ }
    setChoice(c);
  }, []);
  return [choice, set];
}

export function useHashRoute(valid, fallback) {
  const parse = () => {
    const k = (window.location.hash || '').replace(/^#\/?/, '').split('?')[0];
    return valid.includes(k) ? k : fallback;
  };
  const [route, setRoute] = useState(parse);
  useEffect(() => {
    const on = () => setRoute(parse());
    window.addEventListener('hashchange', on);
    return () => window.removeEventListener('hashchange', on);
  }, []); // eslint-disable-line
  const go = useCallback((k) => { window.location.hash = '#/' + k; setRoute(k); }, []);
  return [route, go];
}
