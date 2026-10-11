import { h, F, L, useState, useEffect, useMemo, useCallback } from './h.js';
import { api, setUnauthorizedHandler } from './api.js';
import { setTimeZone } from './format.js';
import { useFetch, useHashRoute, useNarrow, useTheme } from './hooks.js';
import { useLive } from './live.js';
import { Loading, ErrorBox } from './components.js';
import { SignIn } from './views/signin.js';
import { TodayView } from './views/today.js';
import { HourlyView } from './views/hourly.js';
import { TrendsView } from './views/trends.js';
import { AppsView } from './views/apps.js';
import { AlertsView } from './views/alerts.js';
import { SettingsDrawer } from './views/settings.js';
import { AboutDrawer } from './views/about.js';

const { Tabs, Dropdown, Avatar, ActionButton, Menu, Button, EmptyState } = L;

const TABS = [
  { key: 'today', title: 'Today' },
  { key: 'hourly', title: 'Hourly history' },
  { key: 'trends', title: 'Trends' },
  { key: 'apps', title: 'Apps' },
  { key: 'alerts', title: 'Alerts' },
];
const THEMES = [{ key: 'system', label: 'Match system' }, { key: 'light', label: 'Light' }, { key: 'dark', label: 'Dark' }];

export function App() {
  const [session, setSession] = useState(undefined); // undefined = loading
  const refresh = useCallback(() => api.get('/api/session').then(setSession).catch(() => setSession(null)), []);
  useEffect(() => { setUnauthorizedHandler(() => setSession((s) => (s ? { ...s, authenticated: false } : s))); refresh(); }, [refresh]);
  useEffect(() => { if (session && session.timezone) setTimeZone(session.timezone); }, [session]);

  if (session === undefined) return h('p', { className: 'boot' }, 'Loading the dashboard…');
  if (session === null) return h('main', { className: 'signin' }, h('div', { className: 'signin__card' }, h(ErrorBox, { error: { message: 'Cannot reach the service.', hint: 'Check that parent-trackerd is running.' }, onRetry: refresh })));
  if (!session.authenticated) return h(SignIn, { configured: session.configured, onSignedIn: refresh });
  return h(Dashboard, { session, onSignedOut: refresh });
}

function Dashboard({ session, onSignedOut }) {
  const narrow = useNarrow();
  const size = narrow ? 'l' : 'm';
  const [route, go] = useHashRoute(TABS.map((t) => t.key), 'today');
  const [theme, setTheme] = useTheme();
  const users = useFetch('/api/users', {}, { every: 30000 });
  const [user, setUserState] = useState(() => { try { return sessionStorage.getItem('pt-user') || ''; } catch (e) { return ''; } });
  const [date, setDate] = useState(session.today);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [aboutOpen, setAboutOpen] = useState(false);
  const list = users.data ? users.data.users : [];
  const current = list.find((u) => u.name === user) || list[0];
  const name = current ? current.name : '';
  const setUser = (n) => { setUserState(n); try { sessionStorage.setItem('pt-user', n); } catch (e) { /* ignore */ } };

  const live = useLive(name, session.live && !!name);
  const health = useFetch('/api/health', {}, { every: 30000 });
  useEffect(() => { document.title = 'Parent Tracker' + (live.snap && name ? ' — ' + name + ': ' + labelOf(live.snap.state) : ''); }, [live.snap, name]);

  async function signOut() { try { await api.post('/api/logout'); } finally { onSignedOut(); } }

  const header = h('header', { className: 'header' },
    h('div', { className: 'brand' }, h('h1', { className: 'brand__name heading-m' }, 'Parent Tracker')),
    h('div', { className: 'tools' },
      list.length ? h('div', { className: 'tools__user' },
        name ? h(Avatar, { name, size: 'm' }) : null,
        h(Dropdown, { label: 'Showing', items: list.map((u) => ({ key: u.name, label: u.name })), selectedKey: name, onSelectionChange: (k) => { setUser(k); setDate(session.today); }, width: '12rem' })) : null,
      h(ActionButton, { icon: 'sliders', size, onPress: () => setSettingsOpen(true) }, 'Settings'),
      h(ActionButton, { icon: 'help', size, onPress: () => setAboutOpen(true) }, 'About'),
      h(Menu, { label: 'Theme', items: THEMES.map((t) => ({ key: t.key, label: t.label + (theme === t.key ? ' (current)' : '') })), onAction: setTheme }),
      h(ActionButton, { icon: 'log-out', size, onPress: signOut }, 'Sign out')));

  let body;
  if (users.loading && !users.data) body = h(Loading, { label: 'Loading users' });
  else if (users.error) body = h(ErrorBox, { error: users.error, onRetry: users.reload });
  else if (!name) body = h('div', { className: 'screen' }, h(EmptyState, { title: 'No monitored users yet', description: 'Add one on the PC with sudo parent-tracker users add NAME, then reload the service.', illustration: 'empty' }));
  else {
    const shared = { user: name, session };
    const content = {
      today: h(TodayView, { ...shared, live, date, setDate, health: health.data, onOpenHourly: (d) => { setDate(d); go('hourly'); } }),
      hourly: h(HourlyView, { ...shared, date, setDate }),
      trends: h(TrendsView, shared),
      apps: h(AppsView, shared),
      alerts: h(AlertsView, shared),
    };
    body = h('main', { id: 'main' },
      h(Tabs, { 'aria-label': 'Sections', selectedKey: route, onSelectionChange: go, items: TABS.map((t) => ({ ...t, content: content[t.key] })) }));
  }

  return h('div', { className: 'app' },
    h('a', { className: 'skip lm-focusable', href: '#main', onClick: (e) => { e.preventDefault(); const m = document.getElementById('main'); if (m) { m.setAttribute('tabindex', '-1'); m.focus(); } } }, 'Skip to the content'),
    header, body,
    h(SettingsDrawer, { isOpen: settingsOpen, onClose: () => setSettingsOpen(false), editable: session.settings_editable }),
    h(AboutDrawer, { isOpen: aboutOpen, onClose: () => setAboutOpen(false) }));
}

function labelOf(state) {
  return { active: 'Active', idle: 'Idle', locked: 'Locked', screen_off: 'Screen off' }[state] || 'Not signed in';
}
