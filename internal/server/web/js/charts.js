import { h, F, L } from './h.js';
import { fmtDuration, fmtTime, fmtDay, weekdayShort, pad2, pct } from './format.js';

// Charts are built from HTML and tokens, not from a charting library: Lumen has
// none (docs/12 §12.6). States differ by lightness and texture as well as hue,
// every bar is a real button with a text alternative, and "Show as table" gives
// the same numbers (FR-6.26, NFR-8.2).

const STATE_ORDER = ['active', 'idle', 'locked', 'gap'];
export const STATE_NAMES = { active: 'Active', idle: 'Idle', locked: 'Locked', gap: 'Tracking gap' };

export function Legend({ items = STATE_ORDER, extra }) {
  return h('ul', { className: 'legend', 'aria-label': 'Legend' },
    items.map((k) => h('li', { key: k, className: 'legend__item detail-m' }, h('span', { className: 'swatch seg seg--' + k, 'aria-hidden': 'true' }), STATE_NAMES[k])),
    extra);
}

function barLabel(b) {
  const parts = [b.label + ' to ' + fmtTime(b.hour_end) + ':'];
  if (b.future) return parts[0] + ' not reached yet';
  if (b.no_data) return parts[0] + ' no data yet';
  parts.push(fmtDuration(b.active_ms) + ' active', fmtDuration(b.idle_ms) + ' idle', fmtDuration(b.locked_ms) + ' locked');
  if (b.gap_ms > 0) parts.push(fmtDuration(b.gap_ms) + ' not recorded (tracking gap)');
  else if (b.not_signed_in_ms >= b.len_ms) parts.push('not signed in');
  if (b.in_progress) parts.push('hour in progress');
  if (b.bedtime) parts.push('after bedtime');
  return parts.join(', ');
}

// HourlyBars: one stacked bar per hour. A hatched block is a tracking gap
// (unknown, not quiet); empty space is "not signed in"; the hour in progress is
// outlined and grows as time passes.
export function HourlyBars({ buckets, onSelect, selected, compact, label }) {
  const n = buckets.length;
  const step = compact ? 6 : (n > 24 ? 3 : 3);
  return h('div', { className: 'hchart' + (compact ? ' hchart--compact' : '') },
    h('ol', { className: 'hbars', style: { '--cols': n }, 'aria-label': label || 'Usage by hour' },
      buckets.map((b, i) => {
        const len = b.len_ms || 3600000;
        const seg = (k, ms) => (ms > 0 ? h('span', { key: k, className: 'seg seg--' + k, style: { height: Math.min(100, (ms / len) * 100) + '%' } }) : null);
        const cls = ['hbar', b.in_progress ? 'hbar--now' : '', b.future ? 'hbar--future' : '', b.no_data ? 'hbar--nodata' : '',
          b.bedtime ? 'hbar--bed' : '', selected === b.hour_start ? 'hbar--selected' : '', i % step === 0 ? '' : 'hbar--tick-off'].filter(Boolean).join(' ');
        const inner = [
          h('span', { key: 'p', className: 'hbar__plot', 'aria-hidden': 'true' },
            seg('active', b.active_ms), seg('idle', b.idle_ms), seg('locked', b.locked_ms), seg('gap', b.gap_ms)),
          h('span', { key: 'l', className: 'hbar__label detail-s nums', 'aria-hidden': 'true' }, compact ? b.label.slice(0, 2) : b.label.slice(0, 2)),
        ];
        const interactive = onSelect && !b.future && !b.no_data;
        return h('li', { key: b.hour_start },
          interactive
            ? h('button', { type: 'button', className: cls + ' lm-focusable', 'aria-label': barLabel(b), onClick: () => onSelect(b), 'aria-pressed': selected === b.hour_start }, inner)
            : h('span', { className: cls, role: 'img', 'aria-label': barLabel(b), style: { display: 'grid', gap: 'var(--spacing-50)' } }, inner));
      })),
    compact ? null : h('div', { className: 'axis-note detail-s' }, 'Hour of the day, local time. Each bar is one hour: its full height is 60 minutes.'));
}

// level maps active minutes in an hour to a step of the heatmap ramp.
export function level(ms) {
  const m = ms / 60000;
  if (m <= 0) return 0;
  if (m <= 10) return 1;
  if (m <= 25) return 2;
  if (m <= 40) return 3;
  if (m <= 55) return 4;
  return 5;
}
export const LEVEL_NAMES = ['none', '1–10 min', '11–25 min', '26–40 min', '41–55 min', 'more than 55 min'];

export function HeatLegend() {
  return h('ul', { className: 'legend', 'aria-label': 'Active minutes in the hour' },
    [1, 2, 3, 4, 5].map((l) => h('li', { key: l, className: 'legend__item detail-m' }, h('span', { className: 'swatch heat__cell lvl-' + l, 'aria-hidden': 'true' }), LEVEL_NAMES[l])),
    h('li', { className: 'legend__item detail-m' }, h('span', { className: 'swatch seg seg--gap', 'aria-hidden': 'true' }), 'Tracking gap'),
    h('li', { className: 'legend__item detail-m' }, h('span', { className: 'swatch heat__cell heat__cell--bed', 'aria-hidden': 'true' }), 'Activity after bedtime'));
}

function cellLabel(day, c) {
  const when = fmtDay(day) + ', ' + pad2(c.hour) + ':00';
  if (c.skipped) return when + ': this hour does not exist on this day';
  if (c.future) return when + ': not reached yet';
  if (c.no_data) return when + ': no data yet';
  const parts = [fmtDuration(c.active_ms) + ' active'];
  if (c.gap_ms > 0) parts.push(fmtDuration(c.gap_ms) + ' not recorded (tracking gap)');
  if (c.bedtime) parts.push('after bedtime');
  return when + ': ' + parts.join(', ');
}

// Heatmap: rows are days, columns are hours 00–23, intensity is active minutes.
// Hatched cells are tracking gaps; active hours after bedtime are outlined and
// the hour headings of the bedtime window are underlined (FR-6.28).
export function Heatmap({ range, onPick, selected }) {
  const hours = [...Array(24).keys()];
  const bedHours = new Set();
  range.days.forEach((d) => d.cells.forEach((c) => { if (c.bedtime && !c.skipped) bedHours.add(c.hour); }));
  return h('div', { className: 'heat-wrap' },
    h('div', { className: 'heat', role: 'grid', 'aria-label': 'Active time by day and hour' },
      h('span', { className: 'heat__rowlabel' }),
      hours.map((hr) => h('span', { key: 'h' + hr, className: 'heat__hour detail-s nums' + (bedHours.has(hr) ? ' heat__hour--bed' : ''), 'aria-hidden': 'true' }, hr % 3 === 0 ? pad2(hr) : '')),
      range.days.map((d) => h(F, { key: d.date },
        h('span', { className: 'heat__rowlabel detail-s nums' }, weekdayShort(d.date) + ' ' + d.date.slice(8)),
        d.cells.map((c) => {
          const cls = ['heat__cell', 'lm-focusable',
            c.skipped ? 'heat__cell--skip' : '', c.future ? 'heat__cell--future' : '', c.no_data && !c.skipped ? 'heat__cell--nodata' : '',
            !c.skipped && !c.future && !c.no_data ? 'lvl-' + level(c.active_ms) : '',
            c.gap_ms > 0 && !c.active_ms ? 'heat__cell--gap' : '', c.bedtime && c.active_ms > 0 && !c.skipped ? 'heat__cell--bed' : '',
            selected && selected.date === d.date && selected.hour === c.hour ? 'heat__cell--selected' : ''].filter(Boolean).join(' ');
          const inert = c.skipped || c.future || c.no_data;
          return h('button', { key: d.date + c.hour, type: 'button', className: cls, disabled: inert, 'aria-label': cellLabel(d.date, c),
            title: cellLabel(d.date, c), onClick: () => onPick && onPick(d.date, c) });
        }))),
      range.avg_active_ms ? h(F, null,
        h('span', { className: 'heat__rowlabel detail-s', style: { gridColumn: '1 / -1', textAlign: 'start', paddingTop: 'var(--spacing-100)' } }, 'Typical day (average active minutes per hour)'),
        h('span', { className: 'heat__rowlabel detail-s' }, 'Average'),
        range.avg_active_ms.map((ms, hr) => h('span', { key: 'a' + hr, className: 'heat__cell lvl-' + level(ms), role: 'img', 'aria-label': pad2(hr) + ':00 averages ' + fmtDuration(ms) + ' active',
          title: pad2(hr) + ':00 averages ' + fmtDuration(ms) + ' active', style: { cursor: 'default' } }))) : null));
}

// TimelineStrip: the day from 00:00 to 24:00. Blocks are active / idle / locked,
// gaps are hatched, and the line marks "now" (FR-6.13).
export function TimelineStrip({ day, now }) {
  const span = day.day_end - day.day_start;
  const x = (t) => Math.max(0, Math.min(100, ((t - day.day_start) / span) * 100));
  const seg = (s, i, cls) => {
    const left = x(s.start), width = Math.max(0.25, x(s.end) - left);
    return h('span', { key: cls + i, className: 'strip__seg seg seg--' + cls, style: { left: left + '%', width: width + '%' }, title: STATE_NAMES[cls] + ' ' + fmtTime(s.start) + '–' + fmtTime(s.end) });
  };
  const ticks = [0, 6, 12, 18, 24];
  const segs = day.segments.map((s, i) => seg(s, i, s.state === 'screen_off' ? 'locked' : s.state));
  const gaps = day.gaps.map((g, i) => seg(g, i, 'gap'));
  return h('div', { className: 'stack stack--tight' },
    h('div', { className: 'strip', role: 'img', 'aria-label': 'Timeline of the day: ' + day.segments.length + ' blocks of activity' },
      segs, gaps, day.is_today ? h('span', { className: 'strip__now', style: { left: x(now) + '%' }, title: 'Now' }) : null),
    h('div', { className: 'strip__ticks detail-s nums', 'aria-hidden': 'true' },
      ticks.map((t) => h('span', { key: t, className: 'strip__tick', style: { left: (t / 24) * 100 + '%' } }, pad2(t) + ':00'))));
}

// niceTop is the full height of a daily chart: the smallest round duration that
// holds the tallest bar, so a short-use app is not drawn as a sliver.
const TOPS = [10, 15, 30, 60, 120, 180, 240, 360, 480, 720, 1440].map((m) => m * 60000);
function niceTop(max) {
  return TOPS.find((t) => t >= max) || Math.ceil(max / 3600000) * 3600000;
}

// DailyBars: active time per day, with a dashed line at the average.
export function DailyBars({ days, label }) {
  const max = Math.max(1, ...days.map((d) => d.active_ms));
  const withData = days.filter((d) => d.has_data);
  const avg = withData.length ? withData.reduce((s, d) => s + d.active_ms, 0) / withData.length : 0;
  const every = days.length > 14 ? 5 : 1;
  const top = niceTop(max);
  return h('div', { className: 'stack stack--tight' },
    h('ol', { className: 'dbars', style: { '--cols': days.length }, 'aria-label': label || 'Active time per day' },
      days.map((d, i) => h('li', { key: d.date },
        h('span', { className: 'dbar' + (d.has_data ? '' : ' dbar--nodata') + (i % every === 0 ? '' : ' dbar--tick-off'), role: 'img',
          'aria-label': fmtDay(d.date) + ': ' + (d.has_data ? fmtDuration(d.active_ms) + ' active' : 'no data') },
          h('span', { className: 'dbar__plot', 'aria-hidden': 'true' },
            d.has_data ? h('span', { className: 'dbar__fill', style: { height: (d.active_ms / top) * 100 + '%' } }) : null,
            avg > 0 ? h('span', { className: 'dbar__avg', style: { bottom: (avg / top) * 100 + '%' } }) : null),
          h('span', { className: 'dbar__label detail-s nums', 'aria-hidden': 'true' }, days.length > 14 ? d.date.slice(8) : weekdayShort(d.date).slice(0, 2) + ' ' + d.date.slice(8)))))),
    h('div', { className: 'axis-note detail-s' }, 'Full height is ' + fmtDuration(top) + '. Average on days with data: ' + fmtDuration(avg) + '.'));
}

// Donut: a share chart; the legend beside it repeats every value as text.
export function Donut({ items, total }) {
  const r = 40, c = 2 * Math.PI * r;
  let offset = 0;
  const top = items.slice(0, 5);
  const rest = items.slice(5).reduce((s, i) => s + i.ms, 0);
  const list = rest > 0 ? [...top, { name: 'other', ms: rest }] : top;
  const all = list.reduce((s, i) => s + i.ms, 0) || 1;
  return h('div', { className: 'donut' },
    h('svg', { className: 'donut__svg', viewBox: '0 0 100 100', role: 'img', 'aria-label': 'Share of time by category' },
      list.map((it, i) => {
        const len = (it.ms / all) * c;
        const el = h('circle', { key: it.name, className: 'donut__arc slice-' + Math.min(i, 4), cx: 50, cy: 50, r, fill: 'none', strokeWidth: 18,
          strokeDasharray: len + ' ' + (c - len), strokeDashoffset: -offset, transform: 'rotate(-90 50 50)' });
        offset += len;
        return el;
      })),
    h('table', { className: 'donut__table' },
      h('tbody', null, list.map((it, i) => h('tr', { key: it.name },
        h('td', null, h('span', { className: 'swatch slice-' + Math.min(i, 4), 'aria-hidden': 'true' })),
        h('td', { className: 'detail-m' }, it.name),
        h('td', { className: 'detail-m' }, fmtDuration(it.ms) + ' · ' + pct(it.ms, all) + '%'))))));
}

// HourOfDay: a 24-column bar chart of time per hour of the day (typical time of day).
export function HourOfDay({ ms, label }) {
  const max = Math.max(1, ...ms);
  return h('div', { className: 'stack stack--tight' },
    h('ol', { className: 'dbars', style: { '--cols': 24 }, 'aria-label': label || 'Time by hour of the day' },
      ms.map((v, i) => h('li', { key: i },
        h('span', { className: 'dbar' + (i % 3 === 0 ? '' : ' dbar--tick-off'), role: 'img', 'aria-label': pad2(i) + ':00: ' + fmtDuration(v) },
          h('span', { className: 'dbar__plot', 'aria-hidden': 'true' }, v > 0 ? h('span', { className: 'dbar__fill', style: { height: (v / max) * 100 + '%' } }) : null),
          h('span', { className: 'dbar__label detail-s nums', 'aria-hidden': 'true' }, pad2(i)))))),
    h('div', { className: 'axis-note detail-s' }, 'Hour of the day. The tallest bar is ' + fmtDuration(max) + '.'));
}
