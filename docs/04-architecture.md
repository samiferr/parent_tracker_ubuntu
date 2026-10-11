# 04 – Architecture

## 4.1 High-level shape

```
┌──────────────────────────────────────────────────────────────────────┐
│                       Ubuntu desktop PC                              │
│                                                                      │
│  ┌───────────────────────────┐      ┌──────────────────────────────┐ │
│  │ parent-trackerd (systemd) │      │ parent-tracker-agent (per-   │ │
│  │  - session collector      │◄─────┤ user session, GUI access)    │ │
│  │  - activity/idle collector│ D-Bus│  - foreground window         │ │
│  │  - storage + aggregation  │      │  - browser history (opt-in)  │ │
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

A single always-on **system daemon** owns storage and logic. Because Wayland
and per-user GUI data are hard to read from a system daemon, a small
**per-session agent** runs inside the monitored user's graphical session and
reports upwards over a local Unix socket / D-Bus. On X11, the daemon can do
more itself; the agent remains the portable path.

## 4.2 Components

| Component | Privilege | Responsibility |
| --- | --- | --- |
| `parent-trackerd` | root or dedicated `parent-tracker` user | Orchestration, storage, aggregation, retention, alerts, dashboard. |
| `parent-tracker-agent` | monitored user session | GUI-level collection (foreground window), browser history, idle handshake. |
| Session collector | daemon | Consumes systemd-logind / D-Bus session & lock/idle signals. |
| Activity collector | daemon + agent | Builds active/idle timeline. |
| App/window collector | agent (Wayland), daemon or agent (X11) | Foreground app samples. |
| Web collector | daemon (reads browser DBs) or agent | Domain/URL history. |
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
| Session on/off | logind D-Bus | logind D-Bus | — |
| Lock/idle | logind IdleHint, screensaver D-Bus | same | input idle timer |
| Foreground app | `_NET_ACTIVE_WINDOW` via EWMH, `xdotool`/`wmctrl` | GNOME Shell extension or `org.gnome.Shell` focused-app API | PID of focused window; heuristic |
| Window title | X11 property `_NET_WM_NAME` | Shell extension | disabled/optional |
| Screen on/off | DPMS/UPower | same | — |
| Browser history | sqlite read (Firefox `places.sqlite`, Chromium `History`) | same | — |
| Device connect | udev monitor | udev monitor | — |

> **Constraint:** Wayland deliberately prevents arbitrary clients from reading
> other apps' windows. A GNOME Shell extension is the supported route and must
> be installed/enabled per monitored session; without it, Wayland app-level
> tracking degrades to coarse signals (running processes, active-at-login).
> This degradation MUST be visible in `doctor` output and the dashboard.

> **Session types by release.** Ubuntu 24.04 offers both a Wayland and an X11
> GNOME session. Ubuntu 25.10 and later ship the GNOME session on Wayland only
> (X11 applications keep running through XWayland), see the
> [Ubuntu announcement](https://discourse.ubuntu.com/t/ubuntu-25-10-drops-support-for-gnome-on-xorg/62538).
> With 24.04+ as the only supported baseline, the Wayland path is the primary
> one and the X11 path matters mostly on 24.04 and on flavours that still ship
> an X11 session. Wayland app tracking (open question Q2) is therefore a core
> concern, not a corner case.

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

- Daemon watches logind for a monitored user's graphical session start.
- Daemon starts the agent inside that session via systemd user unit or
  `systemd-run --user --scope` with correct `XDG_*`/D-Bus environment.
- Agent opens a Unix-domain socket (owned root) to the daemon, authenticating
  by peer credentials (SO_PEERCRED) and only permitted for the monitored UID.
- If the agent dies, the daemon records a gap and restarts it (bounded retries).

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
- Dashboard: embedded static assets + small JSON API.
- No external runtime deps beyond glibc 2.39 (the Ubuntu 24.04 baseline).

## 4.9 Failure modes & handling

| Failure | Expected handling |
| --- | --- |
| Agent can't start (no GUI libs) | Degrade to session-only; log once; surface in `doctor`. |
| Wayland extension missing | Degrade app tracking; notify admin once. |
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
| GNOME Shell | 46 | Floor for the Wayland GNOME Shell helper/extension (`shell-version` ≥ 46). |
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
  | `app_status` | `ok`, `degraded` (e.g. Wayland without helper) or `off`. |
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
  stopped/crashed, unclean session end); the remainder of the hour is *not in a
  session / PC off* and is derived at read time, not stored. The UI keeps the
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
