import { h, F, L } from '../h.js';
import { useFetch } from '../hooks.js';
import { Loading, ErrorBox } from '../components.js';

const { Drawer, Accordion, Badge, Button } = L;
const STATE = { on: ['informative', 'On'], off: ['neutral', 'Off'], unavailable: ['neutral', 'Not available'] };

// About: what is recorded, who can see it, how long it is kept, and the
// third-party notices (DS-11). It is the same disclosure the child can read.
export function AboutDrawer({ isOpen, onClose }) {
  const { data, error } = useFetch('/api/about', {}, { enabled: isOpen });
  const items = data ? [
    { key: 'recorded', title: 'What is recorded', content: h('ul', { className: 'stack', style: { listStyle: 'none', margin: 0, padding: 0 } },
      data.collected.map((c) => h('li', { key: c.title, className: 'stack stack--tight' },
        h('div', { className: 'badge-row' }, h('strong', { className: 'title-s heading' }, c.title), h(Badge, { variant: STATE[c.state][0] === 'informative' ? 'informative' : 'neutral' }, STATE[c.state][1])),
        h('span', { className: 'muted body-s' }, c.description)))) },
    { key: 'never', title: 'What is never recorded', content: h('ul', { className: 'body-s' }, data.never.map((n) => h('li', { key: n }, n))) },
    { key: 'who', title: 'Who can see it', content: h('div', { className: 'stack' },
      h('p', { className: 'body-s' }, 'You can see the current state and the application in front live, and usage by hour of the day. The monitored person can see everything recorded about them, including that you can see it live, by running parent-tracker self.'),
      h('p', { className: 'body-s' }, 'Nothing leaves this computer. There is no cloud account and no telemetry; this page loads nothing from the internet.')) },
    { key: 'kept', title: 'How long it is kept', content: h('dl', { className: 'kv nums' }, Object.entries(data.retention).map(([k, v]) => h(F, { key: k }, h('dt', null, k), h('dd', null, v)))) },
    { key: 'notices', title: 'Third-party notices', content: h('pre', { className: 'pre detail-s', tabIndex: 0 }, data.notices || 'No notices.') },
  ] : [];
  return h(Drawer, { isOpen, onClose, title: 'About Parent Tracker', width: '34rem', footer: h(Button, { variant: 'secondary', onPress: onClose }, 'Close') },
    h('div', { className: 'drawer-body' },
      error ? h(ErrorBox, { error }) : null,
      !data && !error ? h(Loading, { label: 'Loading' }) : null,
      data ? h(F, null,
        h('p', { className: 'body-s' }, 'Version ' + data.version + '. ' + data.platform),
        h('p', { className: 'body-s muted' }, 'Parent Tracker is for looking after your own child on a computer you own. It is open about what it does: it is never hidden from the person it records.'),
        h(Accordion, { items, defaultExpandedKeys: ['recorded'] })) : null));
}
