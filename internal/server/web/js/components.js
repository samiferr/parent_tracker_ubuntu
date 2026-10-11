import { h, F, L } from './h.js';
import { fmtDuration, fmtTimeSec } from './format.js';

const { StatusLight, InlineAlert, TextField, ProgressBar, Button } = L;

// Session states (docs/12 §12.7). They are never styled as good or bad: the
// positive/negative variants are kept for the health of the tracker itself.
export const STATES = {
  active: { label: 'Active', variant: 'informative' },
  idle: { label: 'Idle', variant: 'neutral' },
  locked: { label: 'Locked', variant: 'purple' },
  screen_off: { label: 'Screen off', variant: 'cyan' },
  suspended: { label: 'Not signed in', variant: null },
  logged_out: { label: 'Not signed in', variant: null },
};

export function StateLight({ state, className }) {
  const s = STATES[state] || STATES.logged_out;
  if (!s.variant) return h('span', { className }, s.label);
  return h(StatusLight, { variant: s.variant }, s.label);
}

export function stateLabel(state) { return (STATES[state] || STATES.logged_out).label; }

// The indicator beside the Now card. Each state is a dot plus a word, so it
// never depends on colour alone (DS-6, FR-6.11).
export function LiveIndicator({ live }) {
  const { status, polling, lastOk } = live;
  let light;
  switch (status) {
    case 'live': light = h(StatusLight, { variant: 'positive' }, 'Live'); break;
    case 'reconnecting': light = h(StatusLight, { variant: 'notice' }, 'Reconnecting…'); break;
    case 'stale': light = h(StatusLight, { variant: 'negative' }, 'Stale — last update ' + fmtTimeSec(lastOk)); break;
    case 'off': light = h('span', { className: 'muted' }, 'Live view off'); break;
    case 'waiting': light = h(StatusLight, { variant: 'notice' }, 'Waiting for the collectors'); break;
    default: light = h('span', { className: 'muted' }, 'Connecting…');
  }
  return h('div', { className: 'live', role: 'status', 'aria-live': 'polite' },
    light,
    status === 'live' && polling ? h('span', { className: 'muted detail-s' }, 'polling') : null);
}

export function Stat({ label, value, note, className }) {
  return h('div', { className: 'stat ' + (className || '') },
    h('span', { className: 'stat__label detail-m' }, label),
    h('span', { className: 'stat__value heading-l' }, value),
    note ? h('span', { className: 'stat__note detail-s' }, note) : null);
}

export function Section({ title, actions, children, level = 2 }) {
  return h('section', { className: 'stack' },
    h('div', { className: 'row row--between' },
      h('h' + level, { className: 'section-title title-l' }, title),
      actions ? h('div', { className: 'inline-actions' }, actions) : null),
    children);
}

export function Loading({ label = 'Loading' }) {
  return h(ProgressBar, { label });
}

export function ErrorBox({ error, onRetry }) {
  if (!error) return null;
  return h(InlineAlert, { variant: 'negative', title: error.message || 'Something went wrong.' },
    h('div', { className: 'stack stack--tight' },
      error.hint ? h('span', null, error.hint) : null,
      onRetry ? h('div', null, h(Button, { variant: 'secondary', size: 's', onPress: onRetry }, 'Try again')) : null));
}

// Lumen has no date picker; a labelled text field of type "date" does the job (docs/12 §12.6).
export function DateField({ label, value, onChange, max, min }) {
  return h(TextField, { label, type: 'date', value: value || '', onChange: (v) => { if (/^\d{4}-\d{2}-\d{2}$/.test(v)) onChange(v); }, width: '11rem',
    ...(max ? {} : {}) });
}

export function Empty({ title, description, children }) {
  return h(L.EmptyState, { title, description, illustration: 'empty', headingLevel: 3 }, children);
}

export const joinClasses = (...c) => c.filter(Boolean).join(' ');
export function Dur({ ms }) { return h('span', { className: 'nums' }, fmtDuration(ms)); }
export { F };

// Code renders `text` with backtick-quoted parts as <code>.
export function Code({ children }) {
  const parts = String(children).split('`');
  return h(F, null, parts.map((t, i) => (i % 2 ? h('code', { key: i, className: 'code' }, t) : t)));
}
