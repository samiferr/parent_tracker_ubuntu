import { h, F, L, useState, useEffect } from '../h.js';
import { useFetch, useNarrow } from '../hooks.js';
import { fmtDuration, fmtTime, fmtLongDay, fmtDay, addDays, pad2 } from '../format.js';
import { Section, Loading, ErrorBox, Empty } from '../components.js';
import { HourlyBars, Heatmap, Legend, HeatLegend } from '../charts.js';
import { DayNav } from './today.js';

const { RadioGroup, Switch, Table, Drawer, InlineAlert, Badge, ActionButton } = L;

const MODES = [{ value: 'day', label: 'Day' }, { value: '7', label: '7 days' }, { value: '30', label: '30 days' }];

function HourDrawer({ bucket, onClose }) {
  const apps = bucket ? bucket.apps || [] : [];
  const title = bucket ? bucket.label + '–' + fmtTime(bucket.hour_end) : '';
  const rows = [['Active', bucket && bucket.active_ms], ['Idle', bucket && bucket.idle_ms], ['Locked', bucket && bucket.locked_ms]];
  return h(Drawer, { isOpen: !!bucket, onClose, title: title || 'Hour', width: '26rem' },
    bucket ? h('div', { className: 'drawer-body' },
      h('p', { className: 'muted body-s' }, fmtLongDay(bucket.day) + (bucket.in_progress ? ' — this hour is still in progress' : '')),
      h('dl', { className: 'kv nums' },
        rows.map(([k, ms]) => h(F, { key: k }, h('dt', null, k), h('dd', null, fmtDuration(ms)))),
        bucket.gap_ms > 0 ? h(F, null, h('dt', null, 'Not recorded'), h('dd', null, fmtDuration(bucket.gap_ms) + ' (tracking gap)')) : null,
        bucket.not_signed_in_ms > 0 ? h(F, null, h('dt', null, 'Not signed in'), h('dd', null, fmtDuration(bucket.not_signed_in_ms))) : null),
      bucket.bedtime ? h(Badge, { variant: 'neutral' }, 'After bedtime') : null,
      h('h3', { className: 'title-m heading' }, 'Top applications'),
      apps.length
        ? h(Table, { 'aria-label': 'Top applications in this hour', density: 'compact',
          columns: [{ key: 'name', title: 'Application', isRowHeader: true }, { key: 'category', title: 'Category' }, { key: 'time', title: 'Time', align: 'end' }],
          rows: apps.map((a) => ({ key: a.app_id, name: a.name, category: a.category, time: fmtDuration(a.foreground_ms) })) })
        : h(Empty, { title: 'No app detail for this hour', description: 'App tracking was unavailable then, or the hour was recorded before per-hour app detail was kept. The time above is still correct.' })) : null);
}

function DayTable({ day }) {
  return h(Table, { 'aria-label': 'Usage by hour', density: 'compact',
    columns: [{ key: 'hour', title: 'Hour', isRowHeader: true }, { key: 'active', title: 'Active', align: 'end' }, { key: 'idle', title: 'Idle', align: 'end' },
      { key: 'locked', title: 'Locked', align: 'end' }, { key: 'gap', title: 'Not recorded', align: 'end' }, { key: 'note', title: 'Note' }],
    rows: day.buckets.map((b) => ({
      key: String(b.hour_start), hour: b.label + '–' + fmtTime(b.hour_end),
      active: b.future || b.no_data ? '—' : fmtDuration(b.active_ms), idle: b.future || b.no_data ? '—' : fmtDuration(b.idle_ms),
      locked: b.future || b.no_data ? '—' : fmtDuration(b.locked_ms), gap: b.gap_ms ? fmtDuration(b.gap_ms) : '—',
      note: b.future ? 'Not reached yet' : b.no_data ? 'No data yet' : b.in_progress ? 'In progress' : b.gap_ms ? 'Tracking gap' : b.not_signed_in_ms >= b.len_ms ? 'Not signed in' : b.bedtime ? 'After bedtime' : '' })) });
}

function RangeTable({ range }) {
  return h(Table, { 'aria-label': 'Active time per day', density: 'compact',
    columns: [{ key: 'day', title: 'Day', isRowHeader: true }, { key: 'total', title: 'Active', align: 'end' }, { key: 'peak', title: 'Busiest hour', align: 'end' }, { key: 'gap', title: 'Not recorded', align: 'end' }],
    rows: range.days.map((d) => {
      const best = d.cells.reduce((m, c) => (c.active_ms > (m ? m.active_ms : 0) ? c : m), null);
      const gap = d.cells.reduce((s, c) => s + c.gap_ms, 0);
      return { key: d.date, day: fmtDay(d.date), total: fmtDuration(d.active_ms), peak: best ? pad2(best.hour) + ':00 (' + fmtDuration(best.active_ms) + ')' : '—', gap: gap ? fmtDuration(gap) : '—' };
    }) });
}

export function HourlyView({ user, session, date, setDate }) {
  const today = session.today;
  const narrow = useNarrow();
  const [mode, setMode] = useState('day');
  const [table, setTable] = useState(false);
  const [selected, setSelected] = useState(null); // hour_start of the open drawer
  const [pending, setPending] = useState(null); // hour to open once a day loads
  const n = mode === 'day' ? 0 : Number(mode);
  const dayQ = useFetch('/api/hourly', { user, date, apps: 1 }, { every: date === today ? 30000 : undefined, enabled: mode === 'day' });
  const rangeQ = useFetch('/api/hourly', { user, from: addDays(date, -(n - 1)), to: date }, { every: date === today ? 60000 : undefined, enabled: mode !== 'day' });
  const day = dayQ.data && dayQ.data.day;
  const range = rangeQ.data && rangeQ.data.range;

  useEffect(() => {
    if (pending == null || !day || day.date !== date) return;
    const b = day.buckets.find((x) => x.hour === pending && !x.future && !x.no_data);
    if (b) setSelected(b.hour_start);
    setPending(null);
  }, [pending, day, date]);

  const bucket = day && day.buckets.find((b) => b.hour_start === selected);
  const err = mode === 'day' ? dayQ.error : rangeQ.error;
  const loading = mode === 'day' ? dayQ.loading && !day : rangeQ.loading && !range;

  return h('div', { className: 'screen' },
    h('div', { className: 'stack stack--tight' },
      h('h2', { className: 'heading-l hero' }, 'Hourly history'),
      h('p', { className: 'muted body-s' }, 'When the PC was used, in the PC’s local time. A hatched block means nothing was recorded, which is not the same as no use.')),
    h('div', { className: 'row' },
      h(RadioGroup, { label: 'View', options: MODES, value: mode, onChange: setMode, orientation: 'horizontal' }),
      h(DayNav, { date, today, onChange: setDate, label: mode === 'day' ? 'Day' : 'Last day' }),
      h('div', { className: 'form__switch' }, h(Switch, { isSelected: table, onChange: setTable }, 'Show as table'))),
    mode !== 'day' ? h('p', { className: 'muted detail-m' }, 'Showing the ' + n + ' days up to ' + fmtDay(date) + '. The day picker sets the last day.') : null,
    err ? h(ErrorBox, { error: err, onRetry: mode === 'day' ? dayQ.reload : rangeQ.reload }) : null,
    loading ? h(Loading, { label: 'Loading the hours' }) : null,

    mode === 'day' && day ? h(F, null,
      h('p', { className: 'body-m hero', 'aria-live': 'polite' }, day.summary),
      day.dst_shift ? h(InlineAlert, { variant: 'informative', title: 'This day has ' + day.buckets.length + ' hours' }, 'Clocks changed on this day, so it is shorter or longer than 24 hours. Each bar is a real hour.') : null,
      table ? h('div', { className: 'table-wrap' }, h(DayTable, { day }))
        : h('div', { className: 'panel stack' },
          h(HourlyBars, { buckets: day.buckets, selected, onSelect: (b) => setSelected(b.hour_start), label: 'Usage by hour for ' + fmtLongDay(day.date) }),
          h(Legend, { extra: day.bedtime ? h('li', { className: 'legend__item detail-m' }, h('span', { className: 'body-s', style: { textDecoration: 'underline' } }, 'Underlined hour'), ' is after bedtime (' + day.bedtime + ')') : null }),
          h('p', { className: 'muted detail-m' }, 'Select an hour to see which applications were in front.'))) : null,

    mode !== 'day' && range ? h(F, null,
      h('p', { className: 'body-m hero', 'aria-live': 'polite' }, range.summary),
      table ? h('div', { className: 'table-wrap' }, h(RangeTable, { range }))
        : h('div', { className: 'panel stack' },
          h(Heatmap, { range, selected: null, onPick: (d, c) => { setMode('day'); setDate(d); setPending(c.hour); } }),
          h(HeatLegend, null),
          h('p', { className: 'muted detail-m' }, 'Select a cell to open that day and hour.'))) : null,

    h(HourDrawer, { bucket, onClose: () => setSelected(null) }));
}
