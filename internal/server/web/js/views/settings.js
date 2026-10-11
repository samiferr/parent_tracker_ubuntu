import { h, F, L, useState, useEffect } from '../h.js';
import { api } from '../api.js';
import { useFetch } from '../hooks.js';
import { Loading, ErrorBox } from '../components.js';

const { Drawer, Switch, TextField, Dropdown, Button, InlineAlert, Disclosure } = L;

const INTERVALS = ['1s', '2s', '5s', '10s', '30s', '1m'];
const CHANNELS = [{ key: 'none', label: 'None' }, { key: 'desktop', label: 'Desktop notification' }, { key: 'ntfy', label: 'ntfy' }, { key: 'webhook', label: 'Webhook' }];

// Settings the dashboard may change (FR-8.3). Who is monitored, the listen
// address, LAN exposure and the password stay on the command line: they decide
// who can be watched and who can see it.
export function SettingsDrawer({ isOpen, onClose, editable }) {
  const { data, error, reload } = useFetch('/api/settings', {}, { enabled: isOpen });
  const [form, setForm] = useState(null);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState(null);
  const [saved, setSaved] = useState(false);

  useEffect(() => { if (data) { setForm({ ...data.values }); setSaved(false); setSaveError(null); } }, [data]);
  useEffect(() => { if (!isOpen) { setSaved(false); setSaveError(null); } }, [isOpen]);

  const set = (k) => (v) => { setForm((f) => ({ ...f, [k]: v })); setSaved(false); };
  const dirty = form && data && Object.keys(form).some((k) => form[k] !== data.values[k]);

  async function save() {
    const changed = {};
    Object.keys(form).forEach((k) => {
      if (form[k] === data.values[k]) return;
      changed[k] = k === 'server.live_max_clients' ? Number(form[k]) : form[k];
    });
    setSaving(true); setSaveError(null);
    try {
      await api.put('/api/settings', { values: changed });
      setSaved(true);
      reload();
    } catch (e) { setSaveError(e); } finally { setSaving(false); }
  }

  const intervals = form && INTERVALS.includes(form.sample_interval) ? INTERVALS : form ? [...INTERVALS, form.sample_interval] : INTERVALS;
  const ro = (data && data.read_only) || {};
  const footer = editable
    ? h(F, null, h(Button, { variant: 'secondary', onPress: onClose }, 'Close'), h(Button, { variant: 'accent', isDisabled: !dirty || saving, onPress: save }, saving ? 'Saving…' : 'Save settings'))
    : h(Button, { variant: 'secondary', onPress: onClose }, 'Close');

  return h(Drawer, { isOpen, onClose, title: 'Settings', width: '30rem', footer },
    h('div', { className: 'form' },
      error ? h(ErrorBox, { error }) : null,
      !form && !error ? h(Loading, { label: 'Loading settings' }) : null,
      !editable && form ? h(InlineAlert, { variant: 'informative', title: 'Settings are read-only here' }, 'Change them on the PC with sudo parent-tracker config set.') : null,
      saveError ? h(InlineAlert, { variant: 'negative', title: saveError.message }, saveError.hint || null) : null,
      saved ? h(InlineAlert, { variant: 'positive', title: 'Settings saved' }, 'They apply now.') : null,
      form ? h(F, null,
        h('fieldset', { className: 'form__group', style: { border: 0, padding: 0, margin: 0 }, disabled: !editable },
          h('legend', { className: 'title-m heading' }, 'What is recorded'),
          h('div', { className: 'form__switch' }, h(Switch, { isSelected: form['collectors.app'], onChange: set('collectors.app'), isDisabled: !editable }, 'Foreground application')),
          h('div', { className: 'form__switch' }, h(Switch, { isSelected: form['collectors.window_title'], onChange: set('collectors.window_title'), isDisabled: !editable }, 'Window titles')),
          form['collectors.window_title'] ? h(InlineAlert, { variant: 'notice', title: 'Titles can show private things' }, 'A title can include a document name or a page title. Your child sees that titles are recorded when they run parent-tracker self.') : null,
          h(Dropdown, { label: 'Sampling interval', description: 'How often the PC is checked.', items: intervals.map((k) => ({ key: k, label: k })), selectedKey: form.sample_interval, onSelectionChange: set('sample_interval'), isDisabled: !editable })),
        h('div', { className: 'form__group' },
          h('h3', { className: 'title-m heading' }, 'Live view'),
          h('div', { className: 'form__switch' }, h(Switch, { isSelected: form['server.live'], onChange: set('server.live'), isDisabled: !editable }, 'Update the dashboard live')),
          h(TextField, { label: 'Most live viewers at once', type: 'number', value: String(form['server.live_max_clients']), onChange: set('server.live_max_clients'), isDisabled: !editable, width: '10rem' })),
        h('div', { className: 'form__group' },
          h('h3', { className: 'title-m heading' }, 'Alerts'),
          h('div', { className: 'form__switch' }, h(Switch, { isSelected: form['alerts.enabled'], onChange: set('alerts.enabled'), isDisabled: !editable }, 'Send alerts')),
          h(Dropdown, { label: 'Where to send them', items: CHANNELS, selectedKey: form['alerts.channel'], onSelectionChange: set('alerts.channel'), isDisabled: !editable }),
          form['alerts.channel'] === 'ntfy' ? h(TextField, { label: 'ntfy topic address', value: form['alerts.ntfy_url'], onChange: set('alerts.ntfy_url'), description: 'Keep the topic name private.', isDisabled: !editable }) : null,
          form['alerts.channel'] === 'webhook' ? h(TextField, { label: 'Webhook address', value: form['alerts.webhook_url'], onChange: set('alerts.webhook_url'), isDisabled: !editable }) : null,
          h(TextField, { label: 'Daily active-time limit', value: form['alerts.daily_active_limit'], onChange: set('alerts.daily_active_limit'), description: 'For example 3h. Use 0s for no limit.', isDisabled: !editable, width: '10rem' }),
          h(TextField, { label: 'Bedtime', value: form['alerts.bedtime'], onChange: set('alerts.bedtime'), description: 'Local time such as 21:00. Leave empty for none.', isDisabled: !editable, width: '10rem' })),
        h('div', { className: 'form__group' },
          h('h3', { className: 'title-m heading' }, 'How long data is kept'),
          [['retention.window_samples', 'Application samples'], ['retention.intervals', 'Activity intervals'], ['retention.hourly_usage', 'Hour-by-hour history'], ['retention.app_usage', 'Daily application totals']].map(([k, label]) =>
            h(TextField, { key: k, label, value: form[k], onChange: set(k), description: 'For example 720h is 30 days.', isDisabled: !editable, width: '10rem' }))),
        h(Disclosure, { title: 'Managed on the command line' },
          h('dl', { className: 'kv' },
            h('dt', null, 'Monitored users'), h('dd', null, (ro.monitored_users && ro.monitored_users.join(', ')) || 'none'),
            h('dt', null, 'Dashboard address'), h('dd', null, ro['server.listen']),
            h('dt', null, 'Open to the home network'), h('dd', null, ro['server.allow_lan'] ? 'Yes' : 'No')),
          h('p', { className: 'muted body-s' }, 'Change these with sudo parent-tracker users, config and setup. They decide who can be watched and who can see it, so they are not editable from the web.'))) : null));
}
