# 08 – Testing & Acceptance

## 8.1 Test levels

| Level | What it covers |
| --- | --- |
| Unit | Aggregation math (active vs idle), retention pruning, rule evaluation, config parsing, app mapping. |
| Integration | Collectors against recorded fixtures (logind signals, X11/Wayland window samples, browser DB snapshots). |
| End-to-end | Real VM: install → monitor a session → see correct numbers in dashboard. |
| Performance | CPU/RAM/disk under 24 h simulated workload; dashboard query latency. |
| Security | File permissions, socket peer auth, dashboard auth, no secret leakage in logs. |
| Packaging | install/upgrade/uninstall on clean 24.04 and the newest supported release, Wayland (and X11 where the release offers it). **Negative test:** installation is refused on 22.04. |
| Live | Scripted state transitions replayed into the live hub; a headless browser (e.g. Playwright) asserts the open dashboard updates with no reload, goes Stale on disconnect, and falls back to polling. |
| Hourly | Interval-splitting and DST/timezone unit tests; daily≡hourly reconciliation over fixture data; migration/backfill on a seeded DB. |
| Compatibility | GNOME primary; KDE/XFCE smoke tests (flavours on 24.04+). |
| Session types | The same scripted scenarios on a **Wayland** and an **X11** GNOME session (24.04), and on the newest release (Wayland only); extension install/enable/disable tests (group K). |
| UI conformance | Lumen conformance: no hard-coded design values, light/dark contrast, zero external requests, local fonts, keyboard/focus/reduced motion, copy rules, phone width (group L). |

## 8.2 Key acceptance criteria

### A. Session & screen time
- **AC-1** Logging in starts a session row; logging out closes it.
- **AC-2** Locking the screen changes state to `locked` within one sample
  interval; active time stops accruing.
- **AC-3** 30 minutes with the screen locked adds **0** active minutes.
- **AC-4** 60 minutes of genuine active use reports 57–63 minutes active.
- **AC-5** Suspend/resume across 2 hours shows a `suspended` interval, not 2 h
  of active time.
- **AC-6** A system clock jump does not produce negative durations.

### B. Application tracking
- **AC-10** With two apps used 10 and 20 minutes foreground, the app breakdown
  reports approximately those values (±10%).
- **AC-11** An app left open but idle all day reports low foreground time.
- **AC-12** On Wayland without the shell extension, the dashboard shows a
  degraded-app-tracking banner (naming the session type and the next step), no
  app rows are written, screen time is still recorded, and nothing crashes.
- **AC-13** Window titles are absent from storage and logs when title capture
  is disabled.

### C. Multi-user
- **AC-20** Only allowlisted users are recorded; a non-monitored account
  produces zero rows.
- **AC-21** Fast user switching attributes time to the correct user.

### D. Reporting
- **AC-30** A parent can determine daily active time and top apps from the
  dashboard without any other tooling.
- **AC-31** Date-range selection returns aggregates consistent with raw data.
- **AC-32** CSV/PDF export matches dashboard figures.

### E. Reliability
- **AC-40** `kill -9` the daemon mid-session; after restart, prior data is
  intact and the interrupted session is closed with a gap marker.
- **AC-41** Pull the plug / hard reboot; no corrupt DB; service auto-starts.
- **AC-42** 7-day soak shows stable memory and no unbounded DB growth.

### F. Security & privacy
- **AC-50** DB/config are not readable by the monitored user.
- **AC-51** Dashboard is unreachable from another LAN host by default.
- **AC-52** Dashboard requires auth; unauthenticated requests are rejected.
- **AC-53** Stopping the service creates a visible "gap"/audit entry in the
  parent's view.
- **AC-54** No credentials appear in logs.

### G. Packaging
- **AC-60** `apt install ./parent-tracker.deb` on a clean 24.04 (and on the
  newest supported Ubuntu release) sets up and enables the service.
- **AC-61** Upgrade preserves `/etc` config and migrates the DB.
- **AC-62** `apt purge` removes the service and binaries; data removal is a
  separate confirmed action.
- **AC-63** On a clean Ubuntu 22.04 image, `apt install ./parent-tracker.deb`
  fails with an unmet-dependency error **before** any file is installed or any
  unit enabled.
- **AC-64** `parent-tracker doctor` reports the OS as supported on 24.04 or
  newer; if run on 22.04 or older it reports "unsupported OS" and exits non-zero.

### H. Privacy defaults
- **AC-70** Screenshots, full-URL capture, and search-term capture are all off
  on a default install.
- **AC-71** First run requires explicit acknowledgement of the disclosure.

### I. Real-time dashboard
- **AC-80** Locking the screen of a test session shows **Locked** on an
  already-open dashboard within one sampling interval + 2 s, with no reload.
- **AC-81** Switching the foreground app updates the Live card within one
  sampling interval + 2 s (X11, or Wayland with the extension). Where app tracking
  is degraded the card shows "App unavailable" and no error.
- **AC-82** The live Active counter stops while locked/idle and resumes on
  unlock; after the next rollup it differs from the stored daily figure by at
  most one sampling interval.
- **AC-83** Stopping the daemon (or cutting the connection) turns the indicator
  to Reconnecting, then Stale, within 3 × the sampling interval; after restart
  it reconnects on its own and shows fresh values. Stale data is never labelled
  Live.
- **AC-84** With streaming blocked or buffered by an intermediary, the dashboard
  falls back to polling `/api/live` and keeps updating.
- **AC-85** Unauthenticated requests to `/api/live` and `/api/live/stream` are
  rejected like any other API route. Opening `live_max_clients + 1` streams
  yields a `503` for the extra client (which falls back to polling) and does not
  disturb sampling cadence or other clients.
- **AC-86** With `collectors.window_title = false` no window title appears in any
  live snapshot or stream event; no field outside the documented payload is sent.
- **AC-87** `server.live = false` plus reload closes open streams with a final
  `bye`, makes `/api/live*` return `404`, and the dashboard shows "Live view
  off"; re-enabling restores streaming without a restart.
- **AC-88** Ten simultaneous live viewers for one hour keep CPU and memory within
  NFR-1.1/1.2 and add no missed samples.

### J. Per-hour history
- **AC-90** An active interval 10:50–11:20 yields 10 min in the 10:00 bucket and
  20 min in the 11:00 bucket.
- **AC-91** For every day in the fixture set, the sum of the day's hourly active
  time equals the daily active time exactly.
- **AC-92** On a spring-forward day the API returns 23 buckets and on a
  fall-back day 25 (two buckets labelled 01:00, distinct `hour_start`); sums
  still match the daily figure. A half-hour-offset zone (e.g. Asia/Kolkata)
  buckets by local hour.
- **AC-93** 30 minutes locked adds 0 active minutes to any hour. An hour that
  overlaps a recorded tracking gap reports `gap_ms`, not quiet usage. A fully
  logged-out hour shows "not signed in", not a gap and not zero-activity.
- **AC-94** After raw samples and intervals have been pruned per retention,
  hourly totals and per-app hourly data are still returned for hours inside
  `retention.hourly_usage`, and are removed after it.
- **AC-95** Two apps used 10 and 20 minutes in the same hour report ≈ those
  values (±10%, same tolerance as AC-10) for that hour.
- **AC-96** Upgrading a database with ≥ 30 days of data backfills `hourly_usage`
  (from intervals) and `hourly_app_usage` (from samples), leaves existing data
  untouched, does not block collection, and is idempotent when re-run.
- **AC-97** Dashboard, `/api/hourly`, `report --by hour` (text and CSV) and
  `parent-tracker self` show identical numbers for the same user and day.
- **AC-98** `delete --user X --before D` removes hourly rows before `D`; the
  export bundle contains them; the child's self-view lists them (FR-10.5).

### K. Session types (Wayland and X11)
- **AC-100** On a GNOME **Wayland** session with the extension enabled, two apps
  used 10 and 20 minutes report ≈ those values (±10%, as AC-10); titles are
  stored only when title capture is on.
- **AC-101** On a GNOME **X11** session (24.04, "Ubuntu on Xorg"), the same
  scenario passes with the same tolerance.
- **AC-102** `sessions.session_type` is correct (`wayland`, `x11`, `tty`) for
  each test session; greeter and SSH sessions produce no rows and no usage.
- **AC-103** Replaying the same scripted lock / idle / suspend sequence on a
  Wayland and on an X11 session yields identical active / idle / locked totals
  (NFR-3.9, FR-3.10).
- **AC-104** On a Wayland session with the extension **disabled**, running one
  native Wayland app and one XWayland app yields **no** foreground-app rows —
  the X11 provider is not used (FR-3.9).
- **AC-105** Disabling the extension (or killing the agent) mid-session produces,
  within 3 × the sampling interval, an `app_sensor_lost` audit entry, the
  dashboard banner and the alert; re-enabling restores app data and logs
  `app_sensor_restored` (SEC-35). Screen time is unaffected throughout.
- **AC-106** Two monitored users in simultaneous sessions of different types
  (one Wayland, one X11) are each attributed correctly (extends AC-21).
- **AC-107** On the newest supported release (no X11 GNOME session) `doctor`
  reports the missing X11 session as expected and passes; the Wayland suite is
  green.
- **AC-108** After installing the package on a Wayland system, the monitored
  user's next login loads the extension with no manual step other than that
  log-out/log-in, and a standard user cannot remove it from their own settings
  (validates the dconf default/lock, 04 §4.13.5).

### L. Lumen design-system conformance
- **AC-110** A CI check finds no hard-coded colour, font size, radius, spacing or
  control-height literals in the dashboard's own CSS/JS (only token references);
  palette steps appear only inside chart/illustration code (DS-2).
- **AC-111** Every screen renders in `light` and `dark`; an automated contrast
  check passes (text ≥ 4.5:1, meaningful graphics and controls ≥ 3:1, except the
  documented Lumen field-border limitation, 12 §12.8) (DS-4, DS-13).
- **AC-112** With all non-loopback network access blocked, every screen loads and
  works; the browser's network log shows **only same-origin requests** — no CDN,
  web-font, icon or analytics host (DS-9, NFR-5.2).
- **AC-113** Fonts are served from the package (Source Sans 3, Source Code Pro);
  the Lumen fallback stack is used if a font file is missing.
- **AC-114** Every interactive element is reachable and operable by keyboard with
  a visible focus ring; with `prefers-reduced-motion` set, no animation runs,
  including live counters and chart transitions (DS-7).
- **AC-115** UI strings pass the copy rules: sentence case, verb buttons, no
  emoji, no exclamation marks, every status shown as a word (DS-5, DS-6).
- **AC-116** The package's `copyright` file lists Lumen/Spectrum-derived values,
  Lucide, React and the fonts with their licences (DS-11, NFR-6.11).
- **AC-117** All screens are usable at 360 px width (no horizontal page scroll,
  controls reachable) (DS-12, NFR-8.8).

## 8.3 Test fixtures & harness

- A "fake session" harness that replays scripted logind/lock/idle transitions
  so tests are deterministic without a real desktop.
- Captured X11 and Wayland signal traces used as golden inputs.
- A disposable VM image with a Wayland session (and an X11 session where the
  release offers one) for E2E.
- Ground-truth script for AC-4/AC-10 that logs actual foreground windows
  independently and compares.
- Timezone fixtures: a DST spring-forward day, a fall-back day, and a half-hour
  offset zone, replayed through the hourly rollup.
- A scripted live-event driver (state/app transitions with controllable timing)
  and a headless-browser test that opens the dashboard once and asserts updates
  arrive without reload.
- A disposable **Ubuntu 22.04** container/VM used only for the negative
  packaging test AC-63/AC-64.
- A fake **foreground provider** that replays scripted app-focus events, so
  aggregation tests do not depend on a real desktop; plus real-desktop E2E runs
  for both the GNOME Wayland extension and the X11 provider.
- A browser-driven UI check (axe-style accessibility + contrast + network-log
  assertions, light/dark, 360 px and desktop widths) for group L.

## 8.4 Quality gates

- Unit + integration tests pass in CI on Ubuntu 24.04 and the newest supported
  release. 22.04 is **not** part of the CI matrix (apart from the negative
  install test).
- Linting/formatting enforced.
- UI conformance checks (group L: AC-110…AC-117) pass for every dashboard change.
- No new P1/P2 security finding open.
- Resource budget check (NFR-1.1/1.2) passes in the performance test.

## 8.5 Manual test matrix

| Dimension | Values |
| --- | --- |
| Ubuntu | 24.04 LTS, newest supported release (22.04 and older: unsupported, install must be refused) |
| Session | Wayland (all releases), X11 (24.04 and any release/flavour that offers it) — both are required, see NFR-3.3 |
| DE | GNOME (primary), KDE (smoke) |
| Arch | x86_64, arm64 |
| Browser (dashboard) | Firefox and a Chromium-family browser on the PC; a phone browser over the LAN |
| Theme | light, dark |
| Scenario | single user, fast-switch (including mixed Wayland/X11), suspend/resume, hard reboot, disk-full, DST day, live reconnect after daemon restart, extension disabled mid-session |
