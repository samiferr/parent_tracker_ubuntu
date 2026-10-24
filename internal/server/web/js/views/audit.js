import { h, F, L, useState } from '../h.js';
import { api } from '../api.js';
import { useFetch, useNarrow } from '../hooks.js';
import { addDays, fmtDay, fmtDateTime, plural } from '../format.js';
import { Loading, ErrorBox, Empty, DateField } from '../components.js';

const { RadioGroup, Table, StatusLight, Badge, Button, InlineAlert } = L;

const PERIODS = [
  { value: 'today', label: 'Today' },
  { value: '7', label: 'Last 7 days' },
  { value: '30', label: 'Last 30 days' },
  { value: 'custom', label: 'Choose dates' },
];
const LEVELS = [
  { value: 'all', label: 'Everything' },
  { value: 'error', label: 'Errors' },
  { value: 'warning', label: 'Warnings' },
  { value: 'info', label: 'Information' },
];
// Each level is a word as well as a colour, so colour is never the only cue.
const LEVEL = {
  error: { label: 'Error', variant: 'negative' },
  warning: { label: 'Warning', variant: 'notice' },
  info: { label: 'Information', variant: 'neutral' },
};

// The period as dates in the PC's calendar: both ends included.
function range(period, today, custom) {
  if (period === 'custom') return custom;
  return { from: period === 'today' ? today : addDays(today, -(Number(period) - 1)), to: today };
}

// The service's own record of what it did and what went wrong: failures to read
// the foreground application, errors in the service, settings and data changes
// (docs/07 §7.2.6). It covers the whole PC, not one monitored user.
export function AuditView({ session }) {
  const narrow = useNarrow();
  const today = session.today;
  const [period, setPeriod] = useState('7');
  const [custom, setCustom] = useState({ from: addDays(today, -6), to: today });
  const [level, setLevel] = useState('all');
  const [exporting, setExporting] = useState('');
  const [exportError, setExportError] = useState(null);

  const { from, to } = range(period, today, custom);
  const valid = from <= to;
  const { data, error, loading, reload } = useFetch('/api/audit', { from, to, level }, { every: 60000, enabled: valid });
  const counts = (data && data.counts) || {};

  async function exportAs(format) {
    setExporting(format);
    setExportError(null);
    try { await api.download('/api/audit', { from, to, level, format }); } catch (e) { setExportError(e); } finally { setExporting(''); }
  }

  const rows = data ? data.entries.map((e) => ({ key: String(e.id), ...e })) : [];
  const periodText = from === to ? fmtDay(from) : fmtDay(from) + ' to ' + fmtDay(to);

  return h('div', { className: 'screen' },
    h('div', { className: 'stack stack--tight' },
      h('h2', { className: 'heading-l hero' }, 'Audit log'),
      h('p', { className: 'muted body-s' }, 'What the service did and what went wrong, for the whole PC: when it could not read the running applications, errors, and changes to settings or data. Saved for the same time as alerts.')),
    h('div', { className: 'row' },
      h(RadioGroup, { label: 'Period', options: PERIODS, value: period, onChange: setPeriod, orientation: narrow ? 'vertical' : 'horizontal' }),
      period === 'custom' ? h(F, null,
        h(DateField, { label: 'From', value: custom.from, onChange: (v) => setCustom((c) => ({ ...c, from: v })) }),
        h(DateField, { label: 'To', value: custom.to, onChange: (v) => setCustom((c) => ({ ...c, to: v })) })) : null),
    h('div', { className: 'row row--between' },
      h(RadioGroup, { label: 'Show', options: LEVELS, value: level, onChange: setLevel, orientation: narrow ? 'vertical' : 'horizontal' }),
      h('div', { className: 'inline-actions' },
        h(Button, { variant: 'secondary', icon: 'download', isDisabled: !valid || !!exporting, onPress: () => exportAs('csv') }, exporting === 'csv' ? 'Exporting…' : 'Export CSV'),
        h(Button, { variant: 'secondary', icon: 'download', isDisabled: !valid || !!exporting, onPress: () => exportAs('json') }, exporting === 'json' ? 'Exporting…' : 'Export JSON'))),
    !valid ? h(InlineAlert, { variant: 'notice', title: 'The end date is before the start date.' }, 'Choose an end date on or after the start date.') : null,
    exportError ? h(ErrorBox, { error: exportError }) : null,
    error ? h(ErrorBox, { error, onRetry: reload }) : null,
    !data && loading && valid ? h(Loading, { label: 'Loading the audit log' }) : null,
    valid && data ? h(F, null,
      h('div', { className: 'badge-row', 'aria-label': 'Counts for ' + periodText },
        h(Badge, { variant: counts.error ? 'negative' : 'neutral' }, plural(counts.error || 0, 'error')),
        h(Badge, { variant: counts.warning ? 'notice' : 'neutral' }, plural(counts.warning || 0, 'warning')),
        h(Badge, { variant: 'neutral' }, (counts.info || 0) + ' information'),
        h('span', { className: 'muted body-s' }, periodText)),
      rows.length === 0
        ? h(Empty, { title: 'Nothing to show', description: level === 'all' ? 'The audit log has no entries for this period.' : 'No entries of this kind for this period.' })
        : h(F, null,
          data.total > rows.length || data.truncated ? h('p', { className: 'muted body-s' },
            'Showing the newest ' + rows.length + ' of ' + data.total + ' entries. An export has all of them' + (data.truncated ? ' that fit in one file; choose a shorter period for the rest.' : '.')) : null,
          h('div', { className: 'panel panel--flush table-wrap' },
            h(Table, { 'aria-label': 'Audit log, newest first', density: 'compact',
              columns: [
                { key: 'time', title: 'Time', isRowHeader: true, render: (r) => h('span', { className: 'nums' }, fmtDateTime(r.ts, true)) },
                { key: 'level', title: 'Level', render: (r) => h(StatusLight, { variant: LEVEL[r.level].variant }, LEVEL[r.level].label) },
                { key: 'title', title: 'Event' },
                { key: 'description', title: 'Details', render: (r) => h('span', { className: 'audit-detail' }, r.description || '—') }],
              rows })))) : null);
}
