import { h, F, L, useState } from '../h.js';
import { useFetch } from '../hooks.js';
import { fmtDuration, addDays, fmtDay } from '../format.js';
import { Section, Loading, ErrorBox, Empty } from '../components.js';
import { DailyBars, HourOfDay } from '../charts.js';

const { RadioGroup, Table, Drawer, ActionButton, Badge } = L;
const RANGES = [{ value: '7', label: '7 days' }, { value: '30', label: '30 days' }];

function AppDrawer({ user, appId, from, to, onClose }) {
  const { data, error } = useFetch('/api/apps/detail', { user, app: appId, from, to }, { enabled: !!appId });
  const a = data && data.app;
  return h(Drawer, { isOpen: !!appId, onClose, title: a ? a.name : 'Application', width: '30rem' },
    h('div', { className: 'drawer-body' },
      error ? h(ErrorBox, { error }) : null,
      !a && !error ? h(Loading, { label: 'Loading the application' }) : null,
      a ? h(F, null,
        h('div', { className: 'badge-row' }, h(Badge, { variant: 'neutral' }, a.category || 'other'), h('span', { className: 'muted body-s' }, fmtDay(from) + ' to ' + fmtDay(to))),
        h('dl', { className: 'kv nums' },
          h('dt', null, 'In front, in use'), h('dd', null, fmtDuration(a.foreground_ms)),
          h('dt', null, 'Open while at the PC'), h('dd', null, fmtDuration(a.open_ms)),
          h('dt', null, 'Visits'), h('dd', null, String(a.sessions))),
        h('h3', { className: 'title-m heading' }, 'Per day'),
        h(DailyBars, { days: fillDays(a.days, from, to), label: 'Time in this application per day' }),
        h('h3', { className: 'title-m heading' }, 'Typical time of day'),
        h(HourOfDay, { ms: a.hour_of_day_ms, label: 'Time in this application by hour of the day' })) : null));
}

function fillDays(points, from, to) {
  const by = {};
  points.forEach((p) => { by[p.date] = p; });
  const out = [];
  for (let d = from; d <= to; d = addDays(d, 1)) out.push({ date: d, active_ms: by[d] ? by[d].foreground_ms : 0, has_data: true });
  return out;
}

export function AppsView({ user, session }) {
  const [range, setRange] = useState('7');
  const [open, setOpen] = useState(null);
  const today = session.today;
  const from = addDays(today, -(Number(range) - 1));
  const { data, error, loading, reload } = useFetch('/api/apps', { user, from, to: today }, { every: 60000 });
  const apps = data && data.apps;

  return h('div', { className: 'screen' },
    h('div', { className: 'stack stack--tight' },
      h('h2', { className: 'heading-l hero' }, 'Applications'),
      h('p', { className: 'muted body-s' }, 'Which applications were in front while the PC was in use. “Open” also counts time the app stayed in front while the PC sat idle.')),
    h(RadioGroup, { label: 'Range', options: RANGES, value: range, onChange: setRange, orientation: 'horizontal' }),
    error ? h(ErrorBox, { error, onRetry: reload }) : null,
    !apps && loading ? h(Loading, { label: 'Loading applications' }) : null,
    apps ? (apps.apps.length === 0
      ? h(Empty, { title: 'No application time recorded', description: 'App tracking may be unavailable or switched off for this user. Screen time is still recorded. Check the banner on the Today tab.' })
      : h(F, null,
        h('p', { className: 'body-m hero' }, fmtDuration(apps.total_foreground_ms) + ' in front across ' + apps.apps.length + ' application' + (apps.apps.length === 1 ? '' : 's') + '.'),
        h('div', { className: 'panel panel--flush table-wrap' },
          h(Table, { 'aria-label': 'Applications', density: 'regular',
            defaultSortDescriptor: { column: 'fg', direction: 'descending' },
            columns: [
              { key: 'name', title: 'Application', isRowHeader: true, allowsSorting: true, render: (r) => h(ActionButton, { isQuiet: true, onPress: () => setOpen(r.key), 'aria-label': 'Details for ' + r.name }, r.name) },
              { key: 'category', title: 'Category', allowsSorting: true, render: (r) => h(Badge, { variant: 'neutral' }, r.category) },
              { key: 'fg', title: 'In front', align: 'end', allowsSorting: true, render: (r) => h('span', { className: 'nums' }, fmtDuration(r.fg)) },
              { key: 'open', title: 'Open', align: 'end', allowsSorting: true, render: (r) => h('span', { className: 'nums' }, fmtDuration(r.open)) },
              { key: 'visits', title: 'Visits', align: 'end', allowsSorting: true, render: (r) => h('span', { className: 'nums' }, String(r.visits)) }],
            rows: apps.apps.map((a) => ({ key: a.app_id, name: a.name, category: a.category, fg: a.foreground_ms, open: a.open_ms, visits: a.sessions })) })))) : null,
    h(AppDrawer, { user, appId: open, from, to: today, onClose: () => setOpen(null) }));
}
