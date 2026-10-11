import { h, F, L, useMemo } from '../h.js';
import { useFetch, useNow, useNarrow } from '../hooks.js';
import { fmtDuration, fmtSince, fmtTime, fmtLongDay, addDays, pct } from '../format.js';
import { StateLight, stateLabel, LiveIndicator, Stat, Section, Loading, ErrorBox, DateField, Empty, Code } from '../components.js';
import { TimelineStrip, HourlyBars, Legend, Donut } from '../charts.js';

const { Card, InlineAlert, Button, ActionButton, Badge } = L;

// DayNav: the day being looked at, with previous / next / today.
export function DayNav({ date, today, onChange, label = 'Day', children }) {
  const narrow = useNarrow();
  const size = narrow ? 'l' : 'm';
  return h('div', { className: 'row' },
    h(DateField, { label, value: date, onChange }),
    h('div', { className: 'inline-actions' },
      h(ActionButton, { icon: 'chevron-left', 'aria-label': 'Previous day', size, onPress: () => onChange(addDays(date, -1)) }),
      h(ActionButton, { icon: 'chevron-right', 'aria-label': 'Next day', size, isDisabled: date >= today, onPress: () => onChange(addDays(date, 1)) }),
      date !== today ? h(Button, { variant: 'secondary', size, onPress: () => onChange(today) }, 'Go to today') : null),
    children);
}

// nowCard shows the live state. Counters advance locally between events, never
// by more than 2.5 sampling intervals past the last sample, and stop entirely
// when the view is stale, so a stalled collector cannot inflate usage and old
// numbers are never presented as current (FR-6.11, FR-6.12).
function NowCard({ live, day, user }) {
  const { snap, status, skew } = live;
  const running = status === 'live' && snap && (snap.state === 'active' || snap.state === 'idle' || snap.state === 'locked');
  const nowTick = useNow(1000, running);
  const stale = status === 'stale' || status === 'reconnecting';
  let totals = { a: day ? day.active_ms : 0, i: day ? day.idle_ms : 0, l: day ? day.locked_ms : 0 };
  let since = 0;
  if (snap) {
    const est = (status === 'live' ? nowTick : snap.last_sample_at) + (status === 'live' ? skew : 0);
    const extra = status === 'live' ? Math.max(0, Math.min(est - snap.last_sample_at, snap.interval_ms * 2.5)) : 0;
    totals = { a: snap.today.active_ms, i: snap.today.idle_ms, l: snap.today.locked_ms };
    if (snap.state === 'active') totals.a += extra; else if (snap.state === 'idle') totals.i += extra; else if (snap.state === 'locked') totals.l += extra;
    since = Math.max(0, est - snap.since);
  }
  let appLine;
  if (!snap) appLine = status === 'off' ? 'The live view is off. Numbers update when you reload.' : 'Waiting for the first update.';
  else if (snap.state === 'logged_out' || snap.state === 'suspended') appLine = 'Not signed in.';
  else if (snap.state === 'locked') appLine = 'The screen is locked.';
  else if (snap.app) appLine = 'In front: ' + snap.app.name + (snap.app.title ? ' — ' + snap.app.title : '');
  else if (snap.app_status === 'degraded') appLine = 'App unavailable.';
  else if (snap.app_status === 'off') appLine = 'App tracking is off for this session.';
  else appLine = 'No app is in front.';

  return h(Card, { title: user + ' now', headingLevel: 2, width: '100%' },
    h('div', { className: 'now' + (stale ? ' is-stale' : '') },
      h('div', { className: 'now__top' },
        h('div', { className: 'now__state' },
          snap ? h('span', { className: 'now__state-label title-l' }, h(StateLight, { state: snap.state })) : null,
          snap && since > 0 && snap.state !== 'logged_out' ? h('span', { className: 'muted body-s nums' }, 'for ' + fmtSince(since)) : null),
        h(LiveIndicator, { live })),
      h('p', { className: 'now__app body-m' }, appLine),
      h('div', { className: 'now__totals' },
        [['Active', totals.a], ['Idle', totals.i], ['Locked', totals.l]].map(([label, ms]) =>
          h('div', { key: label, className: 'stack stack--tight' },
            h('span', { className: 'muted detail-m' }, label + ' today'),
            h('span', { className: 'heading-m nums' }, fmtDuration(ms)))))));
}

function vsUsual(day) {
  if (!day.avg_days) return null;
  const d = Math.round((day.active_ms - day.avg_active_7d_ms) / 60000);
  if (Math.abs(d) < 5) return 'About the usual for the last 7 days';
  return (d > 0 ? '+' : '−') + fmtDuration(Math.abs(d) * 60000) + ' vs usual';
}

function TopApps({ day }) {
  const apps = day.top_apps;
  if (!apps.length) {
    return h(Empty, { title: 'No app time recorded', description: day.apps_available ? 'Nothing was in front while the PC was in use.' : 'App tracking was unavailable or switched off for this day. Screen time is still recorded.' });
  }
  const max = Math.max(...apps.map((a) => a.foreground_ms));
  return h('ol', { className: 'bars', 'aria-label': 'Top applications' },
    apps.map((a) => h('li', { key: a.app_id, className: 'bars__row' },
      h('span', { className: 'bars__name body-s', title: a.name }, a.name, ' ', h('span', { className: 'muted detail-s' }, a.category)),
      h('span', { className: 'bars__track', 'aria-hidden': 'true' }, h('span', { className: 'bars__fill', style: { width: pct(a.foreground_ms, max) + '%', display: 'block' } })),
      h('span', { className: 'bars__value body-s' }, fmtDuration(a.foreground_ms)))));
}

export function TodayView({ user, session, live, date, setDate, onOpenHourly, health }) {
  const today = session.today;
  const isToday = date === today;
  const liveKey = live.snap ? live.snap.state + '|' + (live.snap.app ? live.snap.app.id : '') : '';
  const { data, error, loading, reload } = useFetch('/api/summary', { user, date }, { every: isToday ? 15000 : undefined, refreshKey: isToday ? liveKey : '' });
  const day = data && data.day;
  const nowMs = useNow(30000, isToday);

  const alerts = [];
  if (!session.recording) alerts.push(h(InlineAlert, { key: 'rec', variant: 'notice', title: 'Nothing is being recorded' }, 'The disclosure notice has not been acknowledged. Run sudo parent-tracker setup on the PC.'));
  const uh = health && health.tracker && health.tracker.users && health.tracker.users.find((u) => u.user === user);
  const degraded = (uh && uh.degraded) || (live.snap && live.snap.degraded && live.snap.degraded[0]);
  if (degraded && alerts.length < 2) alerts.push(h(InlineAlert, { key: 'deg', variant: 'notice', title: degraded.reason === 'tty' || degraded.reason === 'unsupported_desktop' ? 'App tracking is not available' : 'App tracking limited' },
    h('span', null, degraded.message + ' ', h(Code, null, degraded.next_step || degraded.nextStep || ''))));

  const totalsDay = useMemo(() => day, [day]);
  return h('div', { className: 'screen' },
    h('div', { className: 'stack stack--tight' },
      h('h2', { className: 'heading-l hero' }, isToday ? 'Today' : fmtLongDay(date)),
      isToday ? h('p', { className: 'muted body-s' }, fmtLongDay(date)) : null),
    h(DayNav, { date, today, onChange: setDate }),
    alerts.length ? h('div', { className: 'stack' }, alerts) : null,
    isToday ? h(NowCard, { live, day: totalsDay, user }) : null,
    error ? h(ErrorBox, { error, onRetry: reload }) : null,
    !day && loading ? h(Loading, { label: 'Loading the day' }) : null,
    day ? h(F, null,
      h('div', { className: 'stats' },
        h(Stat, { label: 'Active time', value: fmtDuration(day.active_ms), note: vsUsual(day) }),
        h(Stat, { label: 'Idle time', value: fmtDuration(day.idle_ms) }),
        h(Stat, { label: 'First activity', value: fmtTime(day.first_activity) }),
        h(Stat, { label: 'Last activity', value: fmtTime(day.last_activity) })),
      day.no_data ? h(Empty, { title: 'Nothing recorded for this day', description: 'The PC was not used, or recording was not running. Pick another day, or check the Alerts tab for tracking gaps.' }) : null,
      h('div', { className: 'grid-2' },
        h(Section, { title: 'Top applications', level: 2 }, h('div', { className: 'panel' }, h(TopApps, { day }))),
        day.categories.length ? h(Section, { title: 'By category', level: 2 },
          h('div', { className: 'panel' }, h(Donut, { items: day.categories.map((c) => ({ name: c.category, ms: c.foreground_ms })) }))) : null),
      h(Section, { title: 'The day', level: 2 },
        h('div', { className: 'panel stack' },
          h(TimelineStrip, { day, now: isToday && live.snap && live.status === 'live' ? live.snap.last_sample_at : nowMs }),
          h(Legend, null))),
      h(Section, { title: 'By hour', level: 2, actions: h(Button, { variant: 'secondary', size: 's', onPress: () => onOpenHourly(date) }, 'Open hourly history') },
        h('div', { className: 'panel' }, h(HourlyBars, { buckets: day.hours, compact: true, label: 'Active time by hour' }), h('div', { className: 'stack stack--tight', style: { marginTop: 'var(--spacing-200)' } }, h(Legend, { extra: day.bedtime ? h('li', { className: 'legend__item detail-m' }, h('span', { className: 'body-s', style: { textDecoration: 'underline' } }, 'Underlined hour'), ' is after bedtime (' + day.bedtime + ')') : null }))))) : null);
}
