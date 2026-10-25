import { h, F, L, useState } from '../h.js';
import { useFetch, useNarrow } from '../hooks.js';
import { fmtDateTime, plural } from '../format.js';
import { Loading, ErrorBox, Empty } from '../components.js';

const { RadioGroup, Timeline, Badge } = L;
const FILTERS = [{ value: 'all', label: 'Everything' }, { value: 'alert', label: 'Alerts' }, { value: 'gap', label: 'Tracking gaps' }, { value: 'audit', label: 'Service and settings' }];

const when = (ms) => fmtDateTime(ms);

// Alerts, tracking gaps and audit entries, newest first (docs/07 §7.2.5). Each
// title names the event, so the dot colour is never the only cue.
export function AlertsView({ user }) {
  const narrow = useNarrow();
  const [filter, setFilter] = useState('all');
  const { data, error, loading, reload } = useFetch('/api/events', { user }, { every: 60000 });
  const events = data ? data.events.filter((e) => filter === 'all' || e.kind === filter) : [];
  const counts = (data && data.counts) || {};

  return h('div', { className: 'screen' },
    h('div', { className: 'stack stack--tight' },
      h('h2', { className: 'heading-l hero' }, 'Alerts and tracking gaps'),
      h('p', { className: 'muted body-s' }, 'The last 30 days. A tracking gap is a time when nothing was recorded, so it is unknown rather than quiet.')),
    h('div', { className: 'row' },
      h(RadioGroup, { label: 'Show', options: FILTERS, value: filter, onChange: setFilter, orientation: narrow ? 'vertical' : 'horizontal' }),
      data ? h('div', { className: 'badge-row', 'aria-label': 'Counts' },
        h(Badge, { variant: 'neutral' }, plural(counts.alert || 0, 'alert')), h(Badge, { variant: 'neutral' }, plural(counts.gap || 0, 'tracking gap')), h(Badge, { variant: 'neutral' }, plural(counts.audit || 0, 'log entry', 'log entries'))) : null),
    error ? h(ErrorBox, { error, onRetry: reload }) : null,
    !data && loading ? h(Loading, { label: 'Loading events' }) : null,
    data ? (events.length === 0
      ? h(Empty, { title: 'Nothing to show', description: filter === 'all' ? 'No alerts, gaps or service events in the last 30 days.' : 'No entries of this kind in the last 30 days.' })
      : h('div', { className: 'panel' }, h(Timeline, { 'aria-label': 'Events, newest first',
        items: events.map((e, i) => ({ key: e.kind + e.ts + i, title: e.title, time: when(e.ts), dateTime: new Date(e.ts).toISOString(),
          description: [e.user ? e.user + ': ' : '', e.description].join('').trim(), variant: e.variant })) }))) : null);
}
