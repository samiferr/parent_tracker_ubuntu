import { h, F, L, useState } from '../h.js';
import { useFetch } from '../hooks.js';
import { fmtDuration, fmtDay, fmtMinuteOfDay, addDays, weekdayShort } from '../format.js';
import { Section, Loading, ErrorBox, Empty } from '../components.js';
import { DailyBars } from '../charts.js';

const { RadioGroup, Switch, Table } = L;
const RANGES = [{ value: '7', label: '7 days' }, { value: '30', label: '30 days' }];

// When does the day start and end? One row per day: a bar from the first to the
// last active moment.
function StartEnd({ days }) {
  const rows = days.filter((d) => d.has_data && d.first_min >= 0);
  if (!rows.length) return h('p', { className: 'muted body-s' }, 'No active days in this range.');
  return h('ol', { className: 'bars', 'aria-label': 'First and last activity per day' },
    rows.map((d) => h('li', { key: d.date, className: 'range-row' },
      h('span', { className: 'body-s nums' }, weekdayShort(d.date) + ' ' + d.date.slice(5)),
      h('span', { className: 'range-track', 'aria-hidden': 'true' },
        h('span', { className: 'range-fill', style: { left: (d.first_min / 1440) * 100 + '%', width: Math.max(0.5, ((d.last_min - d.first_min) / 1440) * 100) + '%' } })),
      h('span', { className: 'body-s nums' }, fmtMinuteOfDay(d.first_min) + '–' + fmtMinuteOfDay(d.last_min)))));
}

export function TrendsView({ user, session }) {
  const [range, setRange] = useState('7');
  const [table, setTable] = useState(false);
  const today = session.today;
  const n = Number(range);
  const { data, error, loading, reload } = useFetch('/api/trend', { user, from: addDays(today, -(n - 1)), to: today }, { every: 60000 });
  const days = data && data.days;
  const withData = days ? days.filter((d) => d.has_data) : [];
  const total = withData.reduce((s, d) => s + d.active_ms, 0);

  return h('div', { className: 'screen' },
    h('div', { className: 'stack stack--tight' },
      h('h2', { className: 'heading-l hero' }, 'Trends'),
      h('p', { className: 'muted body-s' }, 'How active time changes from day to day, and when the day starts and ends.')),
    h('div', { className: 'row' },
      h(RadioGroup, { label: 'Range', options: RANGES, value: range, onChange: setRange, orientation: 'horizontal' }),
      h(Switch, { isSelected: table, onChange: setTable }, 'Show as table')),
    error ? h(ErrorBox, { error, onRetry: reload }) : null,
    !days && loading ? h(Loading, { label: 'Loading the trend' }) : null,
    days ? h(F, null,
      withData.length === 0 ? h(Empty, { title: 'No data in this range', description: 'Nothing was recorded in the last ' + n + ' days.' }) : null,
      withData.length ? h('p', { className: 'body-m hero' }, 'Active ' + fmtDuration(total) + ' over ' + withData.length + ' day' + (withData.length === 1 ? '' : 's') + ', about ' + fmtDuration(total / withData.length) + ' a day.') : null,
      table
        ? h('div', { className: 'table-wrap' }, h(Table, { 'aria-label': 'Active time per day', density: 'compact',
          columns: [{ key: 'day', title: 'Day', isRowHeader: true }, { key: 'active', title: 'Active', align: 'end' }, { key: 'idle', title: 'Idle', align: 'end' }, { key: 'locked', title: 'Locked', align: 'end' }, { key: 'span', title: 'First–last activity', align: 'end' }],
          rows: days.slice().reverse().map((d) => ({ key: d.date, day: fmtDay(d.date), active: d.has_data ? fmtDuration(d.active_ms) : 'No data', idle: d.has_data ? fmtDuration(d.idle_ms) : '—', locked: d.has_data ? fmtDuration(d.locked_ms) : '—',
            span: d.first_min >= 0 ? fmtMinuteOfDay(d.first_min) + '–' + fmtMinuteOfDay(d.last_min) : '—' })) }))
        : h(F, null,
          h(Section, { title: 'Active time per day', level: 2 }, h('div', { className: 'panel' }, h(DailyBars, { days }))),
          h(Section, { title: 'When the day starts and ends', level: 2 }, h('div', { className: 'panel' }, h(StartEnd, { days }))))) : null);
}
