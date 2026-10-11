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
│  │  - dashboard (HTTP)       │      ┌──────────────────────────────┐ │
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
| Aggregator | daemon | Rolls samples into per-app/per-day totals. |
| Retention/pruner | daemon | Enforces retention, compacts. |
| Alert engine | daemon | Evaluates rules, sends notifications. |
| Dashboard | daemon (embedded HTTP) | Read-only + config UI (authenticated). |
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

## 4.4 Data flow

1. Collectors produce **events/samples** with (timestamp, user, type, payload).
2. Samples are buffered and flushed to SQLite in small batches.
3. A periodic aggregator computes derived durations (per app, per state) into
   summary tables.
4. Retention prunes raw events older than the window; aggregates remain.
5. Dashboard/queries read aggregates first, raw events for drill-down.
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
- No external runtime deps beyond a modern glibc.

## 4.9 Failure modes & handling

| Failure | Expected handling |
| --- | --- |
| Agent can't start (no GUI libs) | Degrade to session-only; log once; surface in `doctor`. |
| Wayland extension missing | Degrade app tracking; notify admin once. |
| Browser DB locked/encrypted | Skip cycle; retry; don't crash. |
| Disk full | Stop writes, prune, alert; keep service alive. |
| Clock jump | Clamp/flag intervals; never negative durations. |
| Unclean shutdown | WAL recovery; close open sessions on next start with gap marker. |
