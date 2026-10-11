// Number, duration and time formatting. Times are shown in the PC's timezone
// (the one the hours are bucketed in), not the browser's, so a phone in another
// timezone still reads the same hours as the PC.

let zone;
export function setTimeZone(tz) {
  try { new Intl.DateTimeFormat('en-GB', { timeZone: tz }); zone = tz; } catch (e) { zone = undefined; }
}

const timeFmt = () => new Intl.DateTimeFormat('en-GB', { timeZone: zone, hour: '2-digit', minute: '2-digit', hour12: false });
const secFmt = () => new Intl.DateTimeFormat('en-GB', { timeZone: zone, hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false });

export const fmtTime = (ms) => (ms ? timeFmt().format(new Date(ms)) : '—');
export const fmtTimeSec = (ms) => (ms ? secFmt().format(new Date(ms)) : '—');

export function fmtMinuteOfDay(m) {
  if (m == null || m < 0) return '—';
  return String(Math.floor(m / 60)).padStart(2, '0') + ':' + String(m % 60).padStart(2, '0');
}

// "3 h 05 m", "42 m", "0 m" — digits with tabular figures, as the Lumen copy rules ask.
export function fmtDuration(ms) {
  const m = Math.max(0, Math.round((ms || 0) / 60000));
  if (m >= 60) return Math.floor(m / 60) + ' h ' + String(m % 60).padStart(2, '0') + ' m';
  return m + ' m';
}

// "12 min", "1 h 05 min": for "for 12 min" style phrases.
export function fmtSince(ms) {
  const s = Math.max(0, Math.round(ms / 1000));
  if (s < 60) return s + ' s';
  return fmtDuration(ms).replace(' m', ' min');
}

export function fmtMinutes(ms) {
  const m = Math.round((ms || 0) / 60000);
  return m + ' min';
}

const dayFmt = () => new Intl.DateTimeFormat('en-GB', { timeZone: 'UTC', weekday: 'short', day: 'numeric', month: 'short' });
const longDayFmt = () => new Intl.DateTimeFormat('en-GB', { timeZone: 'UTC', weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' });

// dates are YYYY-MM-DD in the PC's calendar; format them without any timezone shift
const asUTC = (d) => new Date(d + 'T12:00:00Z');
export const fmtDay = (d) => dayFmt().format(asUTC(d));
export const fmtLongDay = (d) => longDayFmt().format(asUTC(d));

export function addDays(d, n) {
  const t = asUTC(d);
  t.setUTCDate(t.getUTCDate() + n);
  return t.toISOString().slice(0, 10);
}

export function weekdayShort(d) {
  return new Intl.DateTimeFormat('en-GB', { timeZone: 'UTC', weekday: 'short' }).format(asUTC(d));
}

export const pad2 = (n) => String(n).padStart(2, '0');
export const hourRange = (b) => b.label + '–' + fmtTime(b.hour_end);
export const pct = (part, whole) => (whole > 0 ? Math.round((part / whole) * 100) : 0);
