# 04 – Architecture

## 4.1 High-level shape

```
┌──────────────────────────────────────────────────────────────────────┐
│                       Ubuntu desktop PC                              │
│                                                                      │
│  ┌───────────────────────────┐      ┌──────────────────────────────┐ │
│  │ parent-trackerd (systemd) │      │ parent-tracker-agent (per-   │ │
│  │  - session collector      │◄─────┤ user session, GUI access)    │ │
│  │  - activity/idle collector│ sock │  - foreground app (X11 or    │ │
│  │  - storage + aggregation  │      │    GNOME Shell ext. relay)   │ │
│  │  - scheduler / retention  │      └──────────────────────────────┘ │
│  │  - alert engine           │                                       │
│  │  - dashboard (HTTP + SSE) │      ┌──────────────────────────────┐ │
│  └───────────┬───────────────┘      │ systemd-logind / GNOME       │ │
│              │                      │  session, idle, lock signals │ │
│              │                      └──────────────────────────────┘ │
│              ▼                                                        │
│      SQLite (WAL)  /var/lib/parent-tracker/tracker.db                 │
│      Config        /etc/parent-tracker/config.toml                    │
└──────────────────────────────────────────────────────────────────────┘
```

A single always-on **system daemon** owns storage and logic. Session state
(active / idle / locked / suspended) comes from **systemd-logind** and does not
depend on the session type. Foreground-app data is GUI-level, so a small
**per-session agent** runs *as the monitored user* inside their graphical
session and reports upwards over a local Unix socket authenticated by peer
credentials. The agent is used for **both** session types: on X11 it reads the
focused window itself; on Wayland it relays what the project's GNOME Shell
extension reports (see [4.13](#413-session-types-wayland-and-x11)). Keeping the
root daemon out of users' X servers and sessions gives one code path, works with
the daemon's sandboxing, and means the daemon never needs a user's X
credentials.

## 4.2 Components

| Component | Privilege | Responsibility |
| --- | --- | --- |
| `parent-trackerd` | root or dedicated `parent-tracker` user | Orchestration, storage, aggregation, retention, alerts, dashboard. |
| `parent-tracker-agent` | monitored user (systemd **user** unit, one per graphical session) | Foreground-app collection for its session (X11 provider, or relay of the GNOME Shell extension), heartbeat to the daemon. Untrusted by design: it runs as the user it observes (see [06](06-security-privacy-and-legal.md), SEC-35). |
| GNOME Shell extension | monitored user's GNOME Shell (Wayland sessions) | Thin sensor: reports focused window changes to the agent. No network, no files, no screen access (see [4.13](#413-session-types-wayland-and-x11)). |
| Session collector | daemon | Consumes systemd-logind session properties and signals (`Type`, `Active`, `IdleHint`, `IdleSinceHint`, `LockedHint`) over the system bus. Independent of session type. |
| Activity collector | daemon | Builds the active/idle/locked/suspended timeline from the session collector. |
| Foreground provider | agent | X11 provider (EWMH) for `x11` sessions; GNOME Wayland provider (extension relay) for `wayland` sessions. Exactly one is active per session. |
| Web collector | daemon (reads browser DBs as root; home directories are not world-readable) | Domain/URL history. |
| Storage layer | daemon | SQLite with WAL, batched writes, migrations. |
| Aggregator | daemon | Rolls samples into per-app/per-day totals **and per-hour buckets** (see [4.12](#412-hourly-rollup)). |
| Live hub | daemon (in-memory) | Holds the latest state per monitored user and fans it out to dashboard clients (see [4.11](#411-live-pipeline-real-time-dashboard)). |
| Retention/pruner | daemon | Enforces retention, compacts. |
| Alert engine | daemon | Evaluates rules, sends notifications. |
| Dashboard | daemon (embedded HTTP) | Read-only + config UI (authenticated); live view over Server-Sent Events. |
| CLI | admin | Manage users/config/export. |

## 4.3 Collection mechanisms per environment

| Signal | X11 | Wayland (GNOME) | Fallback |
| --- | --- | --- | --- |
| Session on/off, type, remote | logind D-Bus (system bus): `Active`, `Type`, `Remote`, `Class` | same | — |
| Lock / idle | logind `LockedHint`, `IdleHint`, `IdleSinceHint` (set by the desktop session; idle onset back-dated to `IdleSinceHint`) | same | input idle timer in the agent, only where the desktop does not maintain `IdleHint` (FR-2.5) |
| Foreground app | Agent reads EWMH `_NET_ACTIVE_WINDOW` (+ `_NET_WM_PID`, `WM_CLASS` / GTK application id) in the user's X session | GNOME Shell extension (`global.display.focus_window` → app via `Shell.WindowTracker`) relayed by the agent | app = "unavailable" (never inferred) |
| Window title | `_NET_WM_NAME` | extension, **only when title capture is enabled** | disabled |
| Screen on/off (SHOULD) | DPMS state | Mutter display power-save state on the user's session bus (validate in Phase 1) | folded into idle/locked |
| Browser history | sqlite read (Firefox `places.sqlite`, Chromium `History`), same on both | same | — |
| Device connect | udev monitor | udev monitor | — |

> **Constraint — why the Wayland route is an extension.** Wayland deliberately
> prevents clients from reading other apps' windows. GNOME Shell's
> `org.gnome.Shell.Introspect` interface *can* list windows, but it only answers
> an allow-list of callers (the XDG desktop portals) unless the shell is in
> unsafe mode, and running as root does not bypass the check (see the
> [GNOME Discourse thread](https://discourse.gnome.org/t/unable-to-call-the-remote-getwindows-gnome-method-via-dbus/21201)).
> Enabling unsafe mode or patching the allow-list would weaken the user's
> desktop and is **not** an acceptable approach. A small GNOME Shell extension,
> which runs inside the shell and may read `focus_window`, is the supported
> route. Without it, Wayland app-level tracking is unavailable (screen time is
> unaffected). This degradation MUST be visible in `doctor` and the dashboard.

> **Which sessions must be supported, and why both:** see
> [4.13](#413-session-types-wayland-and-x11).

## 4.4 Data flow

1. Collectors produce **events/samples** with (timestamp, user, type, payload).
2. Samples are buffered and flushed to SQLite in small batches. In parallel,
   every sample updates the in-memory **live hub** (4.11).
3. A periodic aggregator computes derived durations (per app, per state, **per
   local hour**) into summary tables.
4. Retention prunes raw events older than the window; aggregates remain.
5. Dashboard/queries read aggregates first, raw events for drill-down. The
   **live view** is fed from the live hub, not from the database.
6. Alert engine evaluates rules on a schedule (e.g., every 5 min, nightly).

## 4.5 Session/agent coordination

- The package installs the agent as a **systemd user unit enabled for all
  users** (`/usr/lib/systemd/user/parent-tracker-agent.service`, started with
  `graphical-session.target`). The root daemon does not have to inject
  `DISPLAY`, `XAUTHORITY` or the session bus address into anything: the agent
  starts inside the user's own graphical session with the right environment.
- `graphical-session.target` is only reached by desktops that integrate with
  the systemd user manager (GNOME does). **Xfce (Xubuntu) does not**, so the
  unit alone leaves the agent unstarted there. A second starter, the XDG
  autostart entry `/etc/xdg/autostart/parent-tracker-agent.desktop`, runs on
  every desktop: its launcher (`/usr/lib/parent-tracker/agent-session-start`)
  imports the session's `DISPLAY`/`XAUTHORITY` into the user's service manager
  (a desktop that does not do so itself leaves the unit without them) and starts
  the same unit, falling back to running the agent directly. The agent takes a
  per-user lock (`$XDG_RUNTIME_DIR/parent-tracker-agent.lock`) so that two
  starters never leave two agents closing each other's connection.
- The daemon watches logind for session start (including `Type` and `Class`)
  and decides per session whether it is monitored. The agent connects to the
  daemon's Unix-domain socket (owned by root); the daemon authenticates the
  peer by credentials (`SO_PEERCRED`) and accepts it **only for a monitored UID
  and a non-greeter, non-remote session**. For any other user the connection is
  refused and the agent exits having collected nothing (NFR-5.3: no capture of
  other accounts).
- The agent starts collecting only after the daemon answers "monitored" and
  tells it which provider and options to use (e.g. whether titles are allowed).
- Agent heartbeats every sampling interval. If the agent or the extension goes
  silent, the daemon records a gap in **app** data only (session data from
  logind is unaffected), raises the "app sensor lost" state (SEC-35), and
  relies on systemd to restart the agent (bounded back-off).

## 4.6 Storage

- SQLite in **WAL** mode; one writer (daemon), many readers (dashboard).
- Separate tables for raw events, samples, aggregates, config/audit, and
  alerts. See [05 – Data Model](05-data-model.md).
- Backup = SQLite online backup API on a schedule.

## 4.7 Deployment topology

- MVP: single machine, local-only.
- Optional: admin enables a sync target (another host / NAS) — later phase;
  must be explicit and encrypted in transit.

## 4.8 Technology preferences (non-binding)

- Language: Rust or Go (single static binary, low overhead, good systemd
  integration). Python acceptable for a prototype but watch memory.
- Dashboard: embedded static assets + small JSON API. The front end is a
  **React 18 + Lumen** application ([12](12-design-system-lumen.md)); Lumen,
  React and the fonts are vendored and embedded in the binary (no CDN, DS-9).
  The front-end build tool is a Phase 0 decision; its output is static files.
- No external runtime deps beyond glibc 2.39 (the Ubuntu 24.04 baseline).

## 4.9 Failure modes & handling

| Failure | Expected handling |
| --- | --- |
| Agent can't start or is killed | Degrade to session-only (logind data keeps flowing); record an app-data gap; surface in `doctor` and the dashboard; systemd restarts it with back-off. |
| Wayland extension missing, disabled, or not yet loaded (user has not logged in again since install) | App tracking unavailable for that session; banner "App tracking limited"; one alert; screen time unaffected. |
| X11 server unreachable from the agent | App tracking unavailable for that session; logged once; screen time unaffected. |
| Session type is `tty` or unknown | No GUI provider; session-only. |
| Greeter (login screen) or remote (SSH) session | Never tracked / excluded from screen time. |
| Browser DB locked/encrypted | Skip cycle; retry; don't crash. |
| Disk full | Stop writes, prune, alert; keep service alive. |
| Clock jump | Clamp/flag intervals; never negative durations. |
| Unclean shutdown | WAL recovery; close open sessions on next start with gap marker. |
| Live client slow, stuck or gone | Dropped by the hub after its small buffer fills; never blocks sampling. Client reconnects with back-off, then falls back to polling. |
| Live connection limit reached | `503` + `Retry-After`; client polls `/api/live` instead. |
| Hourly rollup interrupted | Idempotent: the next run recomputes the last 48 h and replaces rows; nothing is double-counted. |
| Unsupported OS (< 24.04) | `apt` refuses the install (versioned dependencies); `doctor` reports "unsupported OS" if the binaries are run anyway. |

## 4.10 Platform baseline: Ubuntu 24.04 and newer

The project supports **one baseline: Ubuntu 24.04 LTS, and everything newer**.
Ubuntu 22.04 LTS and older are out of scope (no builds, no CI, no fixes).

| Component | Baseline (24.04) | Why it matters |
| --- | --- | --- |
| systemd | 255 | logind D-Bus API and unit-hardening directives can be used without fallbacks. |
| glibc | 2.39 | Target for any dynamically linked build; the `.deb` dependency gate. |
| GNOME Shell | 46 | Floor for the Wayland GNOME Shell extension (`shell-version` ≥ 46). GNOME 45 moved extensions to standard ES modules, so the extension is written in ESM only and needs no legacy-import code path. |
| Kernel | 6.8 | No special requirements; listed for test-matrix clarity. |

Consequences:

- **Packaging gate.** The `.deb` declares `Depends: systemd (>= 255)` (and
  `libc6 (>= 2.39)` when dynamically linked). On 22.04 (systemd 249, glibc 2.35)
  `apt install` stops at "unmet dependencies" before any file is placed.
- **No legacy shims.** No code paths, packaging scripts or docs exist solely for
  22.04. When a newer release changes behaviour (e.g. Wayland-only GNOME), the
  project follows the newer behaviour.
- **`doctor`.** Reads `/etc/os-release`. Ubuntu with `VERSION_ID` < 24.04 is
  reported as unsupported; official flavours and derivatives are judged by their
  Ubuntu base (`UBUNTU_CODENAME`) and reported as "best effort" if they are
  based on 24.04 or newer.
- **Upgrading the OS.** A machine still on 22.04 should upgrade Ubuntu to 24.04
  first (`do-release-upgrade`), then install this package. The OS upgrade does
  not touch `/etc/parent-tracker` or `/var/lib/parent-tracker`, so existing
  config and data carry over to the new install.

## 4.11 Live pipeline (real-time dashboard)

```
 tracker loop ──► live hub (memory) ──┬─► GET /api/live/stream   (SSE, primary)
   │              latest state/uid     └─► GET /api/live          (snapshot, fallback)
   └──► batched SQLite writes (unchanged)
```

- **Source.** The tracker loop updates the hub on every sample. Clients never
  cause a database query per tick; the hub keeps today's totals in memory
  (loaded once at start of day / start-up, then maintained incrementally as
  intervals close).
- **Snapshot payload** (also the body of `GET /api/live?user=<name>`):

  | Field | Meaning |
  | --- | --- |
  | `user`, `uid` | Monitored account. |
  | `state`, `since` | `active / idle / locked / screen_off / logged_out`, and when this state began. |
  | `app` | `{id, name}` of the foreground app, plus `title` **only** if `collectors.window_title` is on; `null` when unavailable. |
  | `app_status` | `ok`, `degraded` (e.g. Wayland without the extension) or `off`. |
  | `today` | `{active_ms, idle_ms, locked_ms, gap_ms}` including the in-progress interval, clamped to `last_sample_at`. |
  | `hour` | Current local hour's `{hour_start, active_ms}` (feeds the hourly chart live). |
  | `last_sample_at`, `interval_ms`, `server_time` | Freshness, sampling cadence, and the server clock so the browser can correct skew. |
  | `degraded[]` | Collectors currently degraded (same source as `/api/health`). |

- **Stream** (`GET /api/live/stream?user=<name>`, `text/event-stream`):
  `snapshot` once on connect; `state` when state or app changes; `tick` once
  per sampling interval carrying the totals; a `: keepalive` comment every 15 s.
  Compression is disabled and `X-Accel-Buffering: no` is set so reverse proxies
  on a LAN setup do not hold events back.
- **Fan-out.** Each client gets a small bounded buffer; when it fills, the
  client is dropped (NFR-1.9). Concurrent clients are capped by
  `server.live_max_clients` (default 10, NFR-1.10).
- **Browser behaviour.** Uses `EventSource` (automatic reconnect with
  back-off). After repeated failures or a `503` it switches to polling
  `/api/live` every `max(sample_interval, 5 s)`. If no event/poll succeeds for
  3 × the sampling interval the Live indicator turns to **Stale** (FR-6.11).
  Elapsed-time counters ("active for 12 min") are advanced locally between
  events using `server_time`, and corrected by each tick.
- **Security.** Same authentication as every other `/api/*` route, same origin,
  no CORS. Payload fields obey the same redaction rules as stored data
  (SEC-23). LAN exposure inherits the dashboard's `allow_lan` rules.
- **Switch.** `server.live = false` (reloadable with SIGHUP) closes open
  streams with a final `bye` event and makes `/api/live*` return `404`; the
  dashboard then shows "Live view off" and updates only on reload.

## 4.12 Hourly rollup

- **Sources of truth.** State time per hour comes from `activity_intervals`;
  per-app time per hour from `window_samples` (same attribution rules and
  sample-delta capping as the daily `app_usage` aggregate, so the two always
  reconcile).
- **Splitting.** Every interval is clipped at **local wall-clock hour
  boundaries**. Boundaries are real instants computed from the timezone
  database, so DST days produce 23 or 25 buckets and zones with half-hour
  offsets (e.g. UTC+5:30) bucket correctly. The repeated hour on a "fall back"
  day appears as two distinct buckets with the same hour label.
- **When it runs.** The existing rollup schedule writes rows for each *completed*
  hour and, on every run, recomputes the **last 48 hours** with an idempotent
  upsert (absorbs orphan-interval closure after a crash, late flushes, clock
  corrections). The *current* hour is never stored half-finished: it is computed
  on demand from stored intervals plus the live hub's open interval.
- **Gaps vs. "not signed in".** For each hour: `active`, `idle` and `locked`
  come from intervals; `gap_ms` is overlap with recorded tracking gaps (service
  stopped/crashed, unclean session end); the remainder of the hour is *not
  signed in, suspended or powered off* and is derived at read time, not stored. The UI keeps the
  three apart (FR-6.25). Hours before a user's first recorded interval have no
  rows and render as "no data yet".
- **Migration/backfill.** The release that introduces hourly history adds the two
  tables (additive, NFR-6.9) and backfills in the background: state time from
  `activity_intervals` (up to `retention.intervals`, default 180 days), per-app
  time from `window_samples` (up to `retention.window_samples`, default 30
  days). Older hours therefore have totals but no per-app detail, and the UI says
  so ("app detail available from <date>").
- **API.**
  - `GET /api/hourly?user=<name>&date=YYYY-MM-DD[&apps=1]` → 24 (23/25 on DST
    days) buckets: `hour_start`, `hour`, `active_ms`, `idle_ms`, `locked_ms`,
    `gap_ms`, `hour_len_ms`, and with `apps=1` the top apps for each hour.
  - `GET /api/hourly?user=<name>&from=…&to=…[&shape=grid|avg]` → a
    day × hour grid (heatmap) or the average active time per hour-of-day.
  - Both are authenticated like all `/api/*` routes and read only aggregates
    (NFR-1.11).

## 4.13 Session types: Wayland and X11

### 4.13.1 Why both are required

| Release / flavour | Graphical sessions offered |
| --- | --- |
| Ubuntu 24.04 LTS (GNOME) | **Wayland** (default) and **X11** ("Ubuntu on Xorg", chosen from the login screen) |
| Releases before 25.10 (GNOME) | Both |
| Ubuntu 25.10 and later (GNOME) | **Wayland only**; X11 applications still run through XWayland, but the Xorg GNOME session is gone ([announcement](https://discourse.ubuntu.com/t/ubuntu-25-10-drops-support-for-gnome-on-xorg/62538)) |
| X11-only flavours (e.g. Xubuntu, Ubuntu MATE) | X11 only |
| Kubuntu (KDE Plasma) | X11 and Wayland sessions, depending on the Plasma version |

Consequences, and the policy they lead to:

1. **Wayland is mandatory on every supported release.** It is the default on
   24.04 and the only GNOME session from 25.10 on. A tracker that only worked
   on X11 would see no application data at all on the default session.
2. **X11 is mandatory wherever a supported release or flavour offers it.** 24.04
   users still pick "Ubuntu on Xorg" (remote-desktop tools, some GPU setups,
   legacy apps) and X11-only flavours exist. Supporting it is cheap (EWMH), and
   the same machine can be used in either type from one login to the next.
3. **Neither may affect the core metric.** Screen time comes from
   systemd-logind and must be identical on both (NFR-3.9, FR-3.10).
4. **X11 support is not an error condition where absent.** On a release with no
   X11 session, `doctor` says so as expected (NFR-3.10). Revisit the X11 MUST
   when no supported release or flavour offers an X11 session any more.

### 4.13.2 Support matrix

| Capability | GNOME on Wayland (primary) | GNOME on X11 | KDE Plasma on Wayland | Other X11 desktops (Xfce, MATE, …) | tty / SSH |
| --- | --- | --- | --- | --- | --- |
| Active / idle / locked / suspended time | Yes (logind) | Yes (logind) | Yes (logind) | Yes (logind) | tty: session only; SSH: excluded |
| Live state, hourly state time | Yes | Yes | Yes | Yes | — |
| Foreground app, per-app hourly | Yes, **with the extension**; otherwise unavailable | Yes | No in the first release (FR-3.13) | Yes, with an EWMH-compliant window manager | No |
| Window title (opt-in) | Yes, with the extension | Yes | No | Yes | No |
| Screen-off (SHOULD) | Best effort (display power-save state) | Best effort (DPMS) | No | Best effort (DPMS) | No |

Desktops other than GNOME are best effort (NFR-3.4): they must fail gracefully
and still record screen time.

### 4.13.3 Detection and provider selection

- The daemon reads each session's **`Type`** (`x11`, `wayland`, `tty`, `mir`,
  `unspecified`), **`Class`** (`user`, `greeter`, …), `Desktop` and `Remote`
  from logind. It does not trust environment variables of the daemon; the agent's
  `XDG_SESSION_TYPE` is only a cross-check (on disagreement, trust logind and log).
- Greeter and remote sessions are never tracked (FR-2.9, FR-2.8).
- Provider choice, fixed for the life of the session:

  | `Type` | Desktop | Provider |
  | --- | --- | --- |
  | `x11` | any | X11 provider |
  | `wayland` | GNOME | GNOME Wayland provider (extension) |
  | `wayland` | other | none (screen time only) |
  | `tty`, `mir`, `unspecified` | any | none |

- The type and desktop are stored on the `sessions` row (`session_type`,
  `desktop`, see [05](05-data-model.md)).

### 4.13.4 X11 provider

- Runs in the agent, i.e. inside the user's own X session (correct `DISPLAY` /
  `XAUTHORITY` for free). Reads `_NET_ACTIVE_WINDOW` from the root window, then
  `_NET_WM_PID`, `WM_CLASS` / GTK application id, and `_NET_WM_NAME` (title only
  if allowed). Needs an EWMH-compliant window manager (Mutter, KWin, Xfwm4,
  Marco, …); otherwise app data is "unavailable".
- May speak the X protocol directly or shell out to `xprop` (package `x11-utils`,
  *Recommends*; only needed on systems that offer X11 sessions).
- **Never started on a `wayland` session** (FR-3.9). Under Wayland, X11 tools
  only see XWayland clients, which are not necessarily the focused window, so
  results would be silently wrong.

### 4.13.5 GNOME Wayland provider (Shell extension)

- A small extension written as an ES module (GNOME 45+), with `shell-version`
  entries for the GNOME majors the project has tested (46 and newer). A GNOME
  release the extension does not list will not load it, so each new Ubuntu/GNOME
  release is a tracked compatibility task with its own CI check.
- Behaviour (as built): it owns `org.parenttracker.Sensor` on the user's session
  bus and answers two methods. `GetFocus` looks at the shell's focused window at
  that moment and returns `{available, app id, app name, pid, title}`; the title
  is returned **only while the agent has switched title capture on** with
  `SetTitleCapture`. It does not subscribe to focus changes and keeps nothing
  between calls, so the agent's poll interval is the sampling interval and each
  call doubles as the heartbeat. It has **no network access, writes no files,
  reads no pixels and no input**. It does only what is needed to answer "which
  app is in front?".
- Deployment (to be validated on 24.04 in a Phase 2 spike): files under
  `/usr/share/gnome-shell/extensions/<uuid>/`, enabled through a **system-wide
  dconf default** for `org.gnome.shell enabled-extensions` (the list GNOME would
  use anyway plus this extension, computed at install time by
  `packaging/gnome-extension-setup`), locked so a standard user cannot drop it
  from their own settings. A dconf default is per machine, not per user, so the
  extension loads in every account's session; it is passive and only the agent,
  which collects for monitored accounts only, ever calls it. GNOME Shell loads
  extensions at login and on Wayland cannot be restarted inside a session, so the
  monitored user must **log out and in once** after installation (`doctor` and the
  user guide say so).
- Health: the agent reports the extension state (`active`, `missing`,
  `disabled`, `no-heartbeat`); the daemon audit-logs each transition, shows the
  banner and raises the "app sensor lost" alert (SEC-35).
- Limits: it runs inside the user's shell, so a determined user can disable or
  spoof it. The design aims at **evidence, not enforcement** (SEC-35).
- Rejected alternatives: GNOME unsafe mode or `Eval`; patching the Introspect
  allow-list; screenshot- or portal-based inference (invasive); process-list
  heuristics (cannot tell which app is focused, so never used for attribution).

### 4.13.6 Multiple sessions and users

- Every logind session is tracked independently, each with its own provider and
  health. Only the **active** seat session accrues active time (fast user
  switching, FR-2.7).
- One monitored user may use a Wayland session today and an X11 session
  tomorrow; two monitored users may be in different types at once. Both are
  handled by the per-session design above.
- Degradation banners name the session type, e.g. "App tracking limited in a
  Wayland session — the GNOME Shell extension is not enabled".

### 4.13.7 Validation spikes

- **Phase 1:** confirm on a real 24.04 machine that logind `Type`, `IdleHint`,
  `IdleSinceHint` and `LockedHint` behave the same on the Wayland and the Xorg
  GNOME session; measure the idle delay's effect on AC-4; decide the screen-off
  source.
- **Phase 2:** extension install / enable / lock on 24.04 (including that a
  standard user cannot disable it), behaviour on the newest supported release,
  and the X11 provider on a 24.04 Xorg session.
