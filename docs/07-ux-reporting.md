# 07 – UX & Reporting

## 7.1 Design goals

- A parent gets the key answer (**"how much, and on what?"**) in under a minute.
- No charts-for-charts' sake; default to plain numbers and one clear trend.
- Factual, non-shaming language, especially in the child's self-view.
- "What is happening right now?" is answered at a glance and **updates by
  itself**; "when during the day?" is answered by the hourly view.
- The UI is built entirely with the **Lumen design system** (7.9,
  [12](12-design-system-lumen.md)): consistent, accessible, light and dark, and
  fully self-contained.

## 7.2 Dashboard screens

### 7.2.1 Today (default)
- **Live "Now" card** at the top (updates without reload, FR-6.8):
  - current state as a large label with colour **and** text/icon (Active,
    Idle, Locked, Screen off, Not signed in) and "for 12 min";
  - current foreground app (name, plus title only if title capture is on);
    "App unavailable (Wayland)" when app tracking is degraded;
  - today's running **Active / Idle / Locked** totals, ticking up while active;
  - a **Live** indicator (a `StatusLight` with the word "Live"), turning to
    **Reconnecting…** and then **Stale — last update 14:32:05** if updates stop
    (FR-6.11). Stale numbers are greyed out and never labelled as current.
- Big numbers: **Active time**, **Idle time**, **First activity**, **Last activity**.
- Top 5 apps as a horizontal bar chart with durations.
- Category donut (games / school / social / video / other) if categories configured.
- Day timeline strip showing active / idle / locked blocks; its right edge
  advances with "now" and gaps appear as they happen (FR-6.13).
- A compact 24-hour mini-chart of today by hour, linking to the Hourly history
  screen (7.2.8); the current hour is drawn as "in progress".
- Comparison to 7-day average ("+40 min vs. usual").

### 7.2.2 Trends
- Line/bar of daily active time over 7 / 30 days.
- Stacked per-category trend.
- Bedtime / first-use scatter (when does the day start/end).

### 7.2.3 App detail
- Per-app daily usage, total over range, session count, typical time of day.

### 7.2.4 Web activity (if enabled)
- Top domains, visit counts, time-of-day distribution.
- Clearly labelled as "domain only" unless full-URL mode is on.

### 7.2.5 Alerts & timeline of events
- Rule hits with timestamps; service/tracking gaps highlighted.

### 7.2.5a Audit log
- Own tab beside Alerts. Whole PC, not per user: app-sensor failures (with the
  agent's error text, e.g. X11 or session-bus errors), service warnings and
  errors, settings, user and data changes.
- Period filter (today, 7, 30 days, custom dates; at most 400 days) and level
  filter (error, warning, information). Level is derived from the action
  (`model.AuditLevel`); a sensor that cannot read apps is an error.
- Export of the selected period and level as CSV (UTF-8 with BOM, formulas
  defused) or JSON, through `GET /api/audit?from=&to=&level=&format=csv|json`.
  The screen lists the newest 500 entries; an export has all of them.
- Service warnings and errors are copied into `audit_log` by a `slog` handler
  (`internal/auditlog`), an identical message at most once every 15 minutes.

### 7.2.6 Settings (admin)
- Monitored users, collectors on/off, retention, thresholds, alert channels,
  dashboard auth, LAN exposure toggle (with warning).

### 7.2.7 Child self-view
- Delivered by the CLI (`parent-tracker self`, no root, peer-credential-checked
  local socket). A browser version would need its own per-user authentication
  and is not part of the first release.
- Read-only, same numbers as parent sees for their own account — including the
  hour-by-hour breakdown — with an explainer of what is collected, a plain
  statement that the parent can see current activity live (SEC-6), and how to
  ask for deletion.

### 7.2.8 Hourly history
Answers "when was the PC used, and what was happening at 21:00?".

- **Controls:** user selector, view switch **Day | 7 days | 30 days**, date
  picker. Hours use the local timezone.
- **Day view:** 24 bars (23/25 on DST days). Each bar is the hour's minutes,
  stacked **Active / Idle / Locked**; a hatched overlay marks a **tracking
  gap**; empty space means *not signed in / PC off*. The current hour is drawn
  "in progress" and grows live. Selecting an hour lists its **top apps** with
  minutes and category (empty with a note when app tracking was unavailable
  then, or older than raw-sample retention).
- **Range view (heatmap):** rows are days, columns are hours 00–23, colour
  intensity is active minutes. Hatched cells are tracking gaps; hours after the
  configured bedtime are outlined (FR-6.28). Selecting a cell opens that day and
  hour. A row of per-hour averages sits underneath ("typical day").
- **Plain-language summary line** above the chart, e.g. *"Most use 16:00–19:00 ·
  last activity 22:40 (after bedtime 21:00) · 3 h 05 m active."*
- Never colour-only: every state has a distinct pattern/label, and a **"Show as
  table"** toggle gives the same numbers as a keyboard- and screen-reader-friendly
  table (NFR-8.2).

## 7.3 Reports

| Report | Trigger | Content |
| --- | --- | --- |
| Daily digest | Configurable time (default 21:00) | Active time, top apps, notable events, vs. average |
| Weekly summary | Sunday evening | 7-day trend, category split, bedtime pattern |
| On-demand export | CLI / dashboard | CSV/PDF for a date range |

## 7.4 Alerts

| Rule (example) | Default state | Threshold |
| --- | --- | --- |
| Daily active time exceeded | off | e.g. 3 h |
| Usage after bedtime | off | e.g. after 22:00 |
| Specific app opened | off | configurable |
| New USB/Bluetooth device | off | — |
| Tracking gap / service stopped | **on** | any |
| Foreground-app sensor lost (agent stopped, GNOME extension disabled or missing) | **on** | any (SEC-35) |
| Disk space low | **on** | < 10% |

Channels: local desktop notification, email (SMTP), ntfy/webhook. Rate-limited
and de-duplicated (one alert per rule per window).

## 7.5 CLI UX

```
parent-tracker version
parent-tracker setup                  # users, dashboard login, disclosure acknowledgement
parent-tracker status                 # current sessions (with session type), last sample, counts
parent-tracker users list|add|remove  # manage the monitored-users allowlist
parent-tracker config list|get|set KEY VAL # edit settings
parent-tracker report --from ... --to ... --format csv
parent-tracker report --user alice --by hour [--date 2026-10-09]   # per-hour table
parent-tracker report --user alice --by hour --from ... --to ... --format csv
parent-tracker export --out backup.json
parent-tracker delete --user alice --before 2026-01-01
parent-tracker doctor                 # diagnose OS support, session type, extension, permissions
parent-tracker self [--json]          # child's view, incl. today by hour
parent-tracker serve                  # dashboard only, no collectors (debugging)
parent-tracker completion bash|zsh|fish
parent-tracker secret set smtp-password   # only if an email channel is added later
```

`report --by hour` prints one line per hour that has any data (local time), with
active / idle / locked minutes, gap minutes and the top app, followed by the
day total — the same numbers as the Hourly history screen. `doctor` includes an
**OS support** check (Ubuntu 24.04 or newer, see NFR-3.8).

## 7.6 First-run flow

1. Install package.
2. `parent-tracker setup` asks: which users to monitor? enable web/screenshots?
   alert channel? sets dashboard password.
3. Show disclosure text and require acknowledgement (scripted installs pass an
   explicit acknowledgement flag; never silent).
4. Place a visible "activity is recorded" entry in the child's session and (if
   the child is logged in) show a one-time notification.
5. If a monitored user uses a **Wayland** session, tell the parent that the
   child must **log out and in once** so GNOME Shell loads the extension
   (`doctor` verifies it; until then the dashboard shows "App tracking limited").
6. Open dashboard at `http://127.0.0.1:8787`.

## 7.7 Accessibility & i18n

- Keyboard navigable dashboard, sufficient contrast, semantic HTML.
- English default; strings externalised for later translation.

## 7.8 Error & degraded-state UX

- Dashboard banner (an `InlineAlert`) when a collector is degraded, naming the
  session type and the next step, e.g. "App tracking limited in a Wayland
  session. The GNOME Shell extension is not enabled — ask {user} to log out and
  back in, then run `parent-tracker doctor`."
- On a release that has no X11 session, X11 is never mentioned as a problem.
- Timeline shows explicit **gaps** rather than drawing straight through them.
- Live view states are always explicit: **Live**, **Reconnecting…**, **Stale**,
  **Live view off** (`server.live = false`), and "polling" (shown subtly when
  streaming is unavailable and the fallback is in use).
- Hourly views never turn missing data into zero: tracking gaps are hatched,
  "no data yet" hours (before the first record) are blank with a tooltip.
- `doctor` prints a checklist with fixes, including an unsupported-OS line
  ("Ubuntu 22.04 is not supported — upgrade to 24.04 or newer") and, per
  monitored session, its type and whether its foreground provider is working.

## 7.9 UI implementation: Lumen

The dashboard is built with the Lumen design system; the rules, the
screen-to-component map, the status mapping and the list of pieces Lumen does
not provide (charts, heatmap, date picker, sign-in screen) are in
[12 – Design System: Lumen](12-design-system-lumen.md). In short:

- Components from `window.Lumen` (React 18) and tokens from `tokens.css` only;
  light and dark themes; no other icon set; assets vendored, nothing fetched.
- Copy is sentence case with verb buttons, no emoji or exclamation marks; status
  is always a word plus a dot or icon.
- Session states use non-judgemental `StatusLight` variants; "positive" and
  "negative" are reserved for the health of the tracker itself.
- Top-level navigation is `Tabs` (at most six); settings open in a `Drawer`;
  alerts and gaps are a `Timeline`; banners are `InlineAlert`.
