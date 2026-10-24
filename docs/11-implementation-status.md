# 11 – Implementation Status

> **As of 2026-10-11 the first release is implemented in Go** and builds,
> tests and packages (`make check`, `make deb-all`). It has **not** yet been
> run on a real Ubuntu desktop: everything below that depends on a live GNOME
> Shell, a real systemd service or a real `apt install` is marked as such. A row
> says **Done** only when its code and tests are committed here.

Legend: **Done** (code and automated tests committed), **Partial** (some of the
requirement, the gap is stated), **Not started**, **By design** (the
requirement says not to).

## What has and has not been verified

Verified by automated tests (`make check`: gofmt, `go vet`, the generated Lumen
tokens up to date, the third-party notices up to date, and the whole suite with
the race detector — about 175 test functions):

- **Time maths**: DST-safe local-hour buckets, exact splitting of intervals at
  hour boundaries, daily totals equal to the sum of the 24 hourly buckets,
  23- and 25-hour days, half-hour zones.
- **Tracker**: idle back-dating, suspend/resume, clock jumps, service stop and
  crash recovery, gaps, remote/greeter sessions ignored, nothing recorded
  before the disclosure is acknowledged, app-sensor loss and recovery. It runs
  against a fake session source and, for logind itself, a mock logind on a
  private D-Bus.
- **X11 provider**: against a real X server (Xvfb) with real windows.
- **Agent ↔ daemon**: unix sockets with `SO_PEERCRED`; non-monitored users and
  silent agents are refused or reported.
- **Storage**: migrations (additive, with backup and hourly backfill), retention,
  real deletion (a test proves deleted text is no longer in the database file).
- **Dashboard server**: authentication, login throttling, Host/Origin checks,
  CSP, SSE with a polling fallback, the settings allow-list, the live hub's
  bounded buffers.
- **CLI**: every documented command, including `report`, `export`, `delete`,
  `self` and `doctor`.
- **Front end**: static conformance tests (no hard-coded colours or sizes,
  palette steps only in `charts.css`, no external URLs, no inline scripts, copy
  rules).
- **Packaging**: the `.deb` is built from stub binaries, unpacked into a scratch
  root and run through install, upgrade, remove and purge with the real
  maintainer scripts (dpkg's chrootless mode). The GNOME extension and the agent
  are checked for matching D-Bus names and signatures.

Checked by hand, **not** committed as automated tests: the dashboard was driven
in headless Chromium against `parent-tracker serve --demo` in light and dark
themes at 1280 px and 360 px (no console errors, no cross-origin requests, no
horizontal overflow, every control labelled, focus rings visible, the
"Reconnecting" state when the server goes away). `tools/ui-check/` repeats
those checks.

**Never run in this environment, so unverified:**

- the **GNOME Shell extension** inside a real GNOME Shell (there is none here).
  The agent's side is tested against a mock of its D-Bus interface; the
  `shell-version` list (46–49) is the intended range, not a tested one;
- the **dconf default and lock** on a real Ubuntu desktop (tested against a fake
  `gsettings`), including what the lock does to a user's other extensions;
- the **systemd units** under a real systemd (`systemd-analyze verify` only);
- `apt install` of the `.deb` on a real machine; the arm64 package has been
  built but its binaries were never executed;
- **idle accuracy** (AC-4) on a real GNOME session, and every performance
  target (NFR-1.x) except that the hourly API answers in a few milliseconds on
  30 days of demo data;
- the GitHub Actions workflow in `.github/workflows/ci.yml` has not been run;
- screen readers; contrast is computed from the tokens, not audited with a tool.

## Repository layout (as built)

```
cmd/parent-trackerd/       systemd daemon entry point
cmd/parent-tracker/        administration CLI entry point
cmd/parent-tracker-agent/  per-session agent (systemd user unit)
gnome-extension/           GNOME Shell extension (ESM) - Wayland foreground sensor
internal/
  config/                  TOML config (strict), defaults, validation, settable keys
  model/                   domain types
  storage/                 SQLite (WAL, pure Go), migrations, queries, deletes
  collector/               logind sessions, session-type detection, provider choice
  tracker/                 sampling engine (intervals, samples, gaps, sensor health)
  aggregate/               time maths: hour/day buckets, span clipping, totals
  rollup/                  daily + hourly aggregation, backfill
  live/                    in-memory live hub (SSE fan-out)
  retention/               scheduled pruning, disk check
  alert/                   rule engine + ntfy / webhook / desktop notifiers
  server/                  authenticated dashboard and JSON API
  control/                 control socket for the child's self-view and the CLI
  agent/, agentproto/, agentsrv/, peercred/   agent, its protocol and daemon side
  appmap/                  application id -> name via desktop entries
  view/                    read models shared by the dashboard, CLI and self-view
  daemon/                  wires everything together, reload, sd_notify
  cli/                     CLI commands
  demo/                    made-up data for `parent-tracker serve --demo`
internal/server/web/       dashboard front end (React 18 + Lumen, embedded)
internal/server/web/vendor/ lumen/, react/, fonts/  (vendored, unmodified)
packaging/                 units, tmpfiles, dconf helper, .deb build, notices, example config
tools/lumen-tokens/        compiles Lumen's tokens.json into tokens.css
tools/ui-check/            optional headless-browser checks of the dashboard
```

## Functional requirements (docs/02)

| ID | Requirement | Status | Where / note |
| --- | --- | --- | --- |
| FR-1.1 | systemd system service | Done | `packaging/parent-trackerd.service` (`Type=notify`), `internal/daemon`; unit not yet run under real systemd |
| FR-1.2 | Survives logout/crash | Done | crash recovery from persisted open intervals; tested with injected stops and crashes |
| FR-1.3 | systemctl control | Done | standard unit, `ExecReload` sends SIGHUP |
| FR-1.4 | Config in /etc, not world-readable | Done | 0640, atomic save; `doctor` flags a wider mode |
| FR-1.5 | Data in /var/lib, logs in journal | Done | `StateDirectory`, logs to journald |
| FR-1.6 | Diagnostic mode | Done | `parent-tracker doctor` (reads, writes nothing) |
| FR-1.7 | Health self-report | Done | `collector_health`, `/api/health` (root only), dashboard banner, `STATUS=` in `systemctl status` |
| FR-1.8 | Restart with backoff | Partial | `Restart=on-failure`; no separate "flapping" log line |
| FR-2.1 | Monitored allowlist | Done | `monitored_users`; nothing else is ever recorded |
| FR-2.2 | Session start/end | Done | `sessions` table from logind |
| FR-2.3 | Session states | Partial | active, idle, locked, suspended, logged-out. **Screen-off is not detected** (no logind signal); it counts as idle or locked |
| FR-2.4 | Active time only | Done | `aggregate`, `tracker` |
| FR-2.5 | Idle via logind | Done | `IdleHint` / `IdleSinceHint`, back-dated; accuracy on a real desktop not measured |
| FR-2.6 | Boot/suspend boundaries | Done | `PrepareForSleep`; stop, crash and unclean-logout gaps |
| FR-2.7 | Multiple monitored users | Done | per-user, per-session state; only the active seat session accrues active time |
| FR-2.8 | Remote sessions | Partial | excluded from screen time; not recorded separately |
| FR-2.9 | Per-session type | Done | logind `Type`; greeter and remote sessions never tracked |
| FR-3.1 | X11 foreground window | Done | `internal/agent/x11.go` (EWMH), tested against Xvfb. The agent is started by the systemd user unit (GNOME) **and** by an XDG autostart entry (Xfce/Xubuntu never reach `graphical-session.target`, so before this the agent never ran there); the autostart path has been tested with a fake `systemctl`, not on a real Xubuntu |
| FR-3.2 | Wayland (GNOME) foreground via extension | Partial | extension and agent relay written; **never run in a real GNOME Shell** |
| FR-3.3 | App id/name mapping | Done | `internal/appmap` (system, Flatpak and snap desktop entries) |
| FR-3.4 | Per-app daily aggregation | Done | `rollup`, `app_usage` |
| FR-3.5 | Window title (optional) | Done | off by default; the extension returns titles only while the agent turns capture on |
| FR-3.6 | Idle-with-app-open split | Done | "in front" versus "open" time |
| FR-3.7 | Categories | Done | `[[categories]]`, first match wins |
| FR-3.8 | Full-screen/game foreground | Not started | no special handling |
| FR-3.9 | X11 provider never on Wayland | Done | provider chosen per session type |
| FR-3.10 | Core metrics identical on Wayland and X11 | Done | session collector is session-type independent (tested) |
| FR-3.11 | Visible degradation | Done | `doctor`, `/api/health`, Today-screen banner naming the session type |
| FR-3.12 | Extension installed system-wide, enabled, loss logged | Partial | package ships the extension and a locked dconf default; loss is audited and alerted. Enablement is **untested on real GNOME** |
| FR-3.13 | KDE Wayland foreground | Not started | those sessions record screen time only |
| FR-4.1–4.4 | Web history, search queries | Not started | the `collectors.web*` keys exist; turning them on records nothing, and the CLI, `doctor` and the service log say so |
| FR-5.x | Screenshots | Not started | key exists, never collected |
| FR-6.1–6.4 | Dashboard, summaries, timeline, auth, loopback | Done | `internal/server`, `internal/server/web` |
| FR-6.5 | CSV/PDF export | Partial | CSV from `report --format csv`, JSON from `export`; **no PDF** |
| FR-6.6 | Daily digest | Not started | |
| FR-6.7 | Budgets | Not started | |
| FR-6.8–6.15 | Real-time dashboard | Done | `internal/live`, SSE, polling fallback, Live / Stale / Reconnecting |
| FR-6.16 | Tab badge | Partial | the tab title shows the state; no favicon badge |
| FR-6.20–6.28 | Per-hour history | Done | `hourly_*` tables, `GET /api/hourly`, `report --by hour`, bedtime marking |
| FR-6.29 | Typical-week view | Partial | the heatmap has a "typical day" row; no per-weekday view |
| FR-7.1 | Alert conditions | Partial | daily limit, bedtime, tracking gap, sensor loss, low disk. No "app X used" or "new device" rule |
| FR-7.2 | Alert channels | Partial | desktop, ntfy, webhook; **no email** |
| FR-7.3 | Rate limiting | Done | de-duplicated within the hour |
| FR-7.4 | Weekly digest | Not started | |
| FR-8.1–8.2 | CLI, reload | Done | `internal/cli`, SIGHUP |
| FR-8.3 | Dashboard settings | Partial | a safe subset (recording, live view, alerts, retention). Users, listen address, LAN access and the password are CLI-only on purpose |
| FR-8.4 | Safe defaults, never silently track a new user | Done | |
| FR-8.5 | `doctor` | Done | OS, systemd, config, database, service, logind, per-session provider |
| FR-8.6 | Uninstall | Done | `apt remove` keeps data, `apt purge` deletes it (tested in a scratch root) |
| FR-9.x | Enforcement | Not started | deferred (Phase 6) |
| FR-10.1–10.2 | Retention, export, delete | Done | |
| FR-10.3 | Child self-view | Done | `parent-tracker self`, no root needed |
| FR-10.4 | Real deletion | Done | rows deleted, then `VACUUM` |
| FR-10.5 | Hourly data in export/delete/self-view | Done | |

## Non-functional requirements (docs/03)

| ID | Requirement | Status | Notes |
| --- | --- | --- | --- |
| NFR-1.1–1.4, 1.6, 1.7 | CPU, RAM, I/O, UI latency, page load, battery | Not started | **not measured**. Writes are batched per tick in WAL mode |
| NFR-1.5 | Interval 1–60 s | Done | validated |
| NFR-1.8–1.10 | Live-view cost, never blocks sampling, bounded clients | Done | state served from memory; slow clients dropped; `live_max_clients` |
| NFR-1.11 | Hourly queries < 300 ms | Done | a few ms on 30 days of demo data |
| NFR-2.1, 2.3, 2.4 | No loss on unclean stop, clean restart, clock changes | Done | |
| NFR-2.2 | Collector isolation | Partial | sensor and agent failures degrade without stopping the tracker; there is no catch-all panic guard |
| NFR-2.5, 2.6 | Long-run memory, watchdog | Not started | |
| NFR-3.1, 3.7, 3.8 | Ubuntu 24.04+ only | Done | `Depends: systemd (>= 255)` makes `apt` refuse older releases; `doctor` and the service report an unsupported OS |
| NFR-3.2 | GNOME Shell 46+ | Partial | extension lists 46–49; untested |
| NFR-3.3, 3.9, 3.10 | Wayland and X11 | Partial | both providers exist and core metrics are session-independent; only the X11 provider has run against a real display server |
| NFR-3.4 | Other desktops degrade gracefully | Done | screen time only, with a banner |
| NFR-3.5, 6.10 | amd64 and arm64 | Partial | both packages build (static Go, `CGO_ENABLED=0`); arm64 never executed |
| NFR-3.6 | No X11 tooling needed | Done | the X protocol is spoken in Go |
| NFR-4.1 | Least privilege | Partial | the daemon runs as root (it must not be stoppable by the monitored account); the agent runs as the user |
| NFR-4.2, 4.3, 4.4 | File modes, loopback + auth, redacted logs | Done | |
| NFR-4.5 | Encryption at rest | Not started | |
| NFR-4.6 | systemd hardening | Partial | `NoNewPrivileges`, `ProtectSystem=strict`, kernel and cgroup protection, `MemoryDenyWriteExecute`. A capability bound and syscall filter were left out because they cannot be validated without a real systemd, and the desktop-alert channel needs `runuser` |
| NFR-4.7 | Tamper evidence | Done | audit log (own dashboard tab, filter by period and level, CSV and JSON export; verified in headless Chromium and by tests, not on a real desktop), gaps, sensor-loss alerts, which are recorded even when alerts are off |
| NFR-5.1–5.5 | Local-only, no telemetry, allowlist, self-view | Done | the dashboard loads nothing from another origin (tested) |
| NFR-6.1–6.3 | `.deb`, upgrade safety, purge | Done | install, upgrade, remove, purge tested in a scratch root |
| NFR-6.4, 6.9 | Migrations with backup, hourly backfill | Done | |
| NFR-6.5 | Open config format | Done | TOML, annotated example |
| NFR-6.6 | CI | Partial | `.github/workflows/ci.yml` written, not yet run |
| NFR-6.7 | Signed releases | Not started | |
| NFR-6.8 | Versioned dependencies | Done | `systemd (>= 255)`; `libc6` not needed (static) |
| NFR-6.11 | Third-party notices | Done | `packaging/gen-notices.sh`, installed as the package `copyright` file |
| NFR-7.1–7.3 | Logs, rotation, `status` | Done | |
| NFR-7.4 | Disk guard | Partial | pruning plus a low-disk alert; collection is not stopped |
| NFR-8.1, 8.8 | Responsive, usable on a phone | Done | checked at 360 px |
| NFR-8.2 | Accessibility | Partial | keyboard, labels and focus rings checked; **no screen-reader testing** |
| NFR-8.3 | Non-shaming | Done | no good/bad styling of activity |
| NFR-8.4 | Localisation | Not started | English only |
| NFR-8.5–8.7 | Lumen, self-hosted assets, themes | Done | see the design-system table |
| NFR-9.1–9.3 | Disclosure, retention docs, child explainer | Done | `setup`, `notice`, `self` |

## Security and privacy requirements (docs/06)

| IDs | Status | Notes |
| --- | --- | --- |
| SEC-1, 3–6, 40–41, 43 | Done | disclosure acknowledgement gates recording; `self` shows everything including live and hourly |
| SEC-2 | Partial | autostart notice and a menu entry ("What is recorded about me") are installed; not seen on a real desktop |
| SEC-10–16 | Done | loopback by default; bcrypt login with throttling; root-only admin CLI; peer-credential socket; audited changes; live and hourly APIs behind the same auth; live data obeys collector switches. No TOTP |
| SEC-17 | Partial | the extension is passive, has no network, file or input access (checked statically); not verified inside GNOME Shell |
| SEC-20, 22, 23 | Done | file modes; secrets never logged or audited; titles redacted |
| SEC-24 | Partial | exports are written 0600; there is no warning text |
| SEC-21, 33 | Not started | encryption at rest; integrity hash |
| SEC-30–32, 34, 35 | Done | stop, collector-off and gaps are surfaced; the service is re-enabled on upgrade; the trust boundary is documented |
| SEC-42 | By design | the sensitive collectors do not exist in this release |

## Design system (docs/12)

| IDs | Status | Notes |
| --- | --- | --- |
| DS-1–3, 9, 10 | Done | Lumen only, vendored unmodified at `LUMEN_VERSION`; conformance tests in `internal/server/web/web_test.go` |
| DS-4, 7 | Done | light and dark follow the OS, with a manual override; 2 px focus ring; reduced motion respected |
| DS-5, 6 | Done | sentence case, verb buttons, status as a word plus a dot |
| DS-8 | Done | icons only from Lumen's `Icon` |
| DS-11 | Done | notices ship in the package |
| DS-12 | Done | larger control sizes and a stacked layout on narrow screens |
| DS-13 | Partial | chart colours were chosen to reach 3:1 in both themes by computation, and every series also has a pattern; no external audit |
| DS-14 | By design | nothing was proposed back to Lumen |

## Acceptance criteria (docs/08)

No per-AC traceability matrix has been built. The automated tests above cover
most of the functional criteria. The ones that need a real machine have **not**
been run: idle accuracy on a real GNOME session (AC-4), the extension's install,
enable and lock on 24.04, the X11 provider under a real Xorg session, the
performance targets, and package install and upgrade on a real `apt` system.

## Roadmap phases (docs/09)

| Phase | Status |
| --- | --- |
| 0 Foundations | Done (repository layout, CI file, `.deb` for amd64 and arm64, Lumen vendored) |
| 1 MVP: session & screen time | Done in code; the real-machine validation spike has not been run |
| 2 Application tracking | Partial: X11 provider done; Wayland extension written but unverified |
| 3 Reporting, alerts, multi-user polish | Partial: dashboard, CSV, alerts done; digest, email and PDF not |
| 4 Web activity & devices | Not started |
| 5 Hardening & packaging polish | Partial: packaging and tamper evidence done; hardening and CI incomplete |
| 6 Enforcement | Not started |
| 7 Nice-to-haves | Not started |
| 8 Platform baseline, hourly history, live dashboard | Done |

## Where the implementation differs from the earlier specification

- `window_samples` has an extra `app_name` column so a display name survives
  after the desktop entry is gone.
- Tamper-evidence alerts (tracking gap, app sensor lost or unavailable, low
  disk) are recorded even when `alerts.enabled` is false; only delivery and the
  limit and bedtime rules depend on it.
- The CLI takes `-c/--config` after the command (`parent-tracker doctor -c FILE`).
- `parent-tracker config list` prints strings in single quotes (TOML literal
  strings), which the loader accepts.
- `parent-tracker serve --demo` runs the dashboard on made-up data in a
  temporary directory; nothing real is read or written.
- The GNOME extension answers on request (a D-Bus method) instead of watching
  focus changes, so it keeps nothing between calls.

## Biggest open risks

1. **Wayland foreground app.** The extension has never run in a real GNOME
   Shell, and each GNOME release may need changes (docs/04 §4.13.5).
2. **The dconf lock.** Locking `enabled-extensions` stops a child dropping the
   extension, and also stops them managing other extensions; the default list is
   computed at install time from the schema, which is untested on a real system.
3. **Idle accuracy.** Idle comes from the desktop's idle hint (AC-4 depends on
   the desktop's idle delay); it needs the Phase 1 spike on a real session.
4. **Screen-off detection** has no logind signal and is folded into idle or
   locked time (FR-2.3 makes it SHOULD).
5. **systemd hardening** is conservative because it could not be tried under a
   real systemd; tighten it once it can.
