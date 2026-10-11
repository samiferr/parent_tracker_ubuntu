# 11 – Implementation Status

> **As of 2026-10-11 this repository contains no code** — specification and
> documentation only. **Every requirement below is "Not started".** Earlier
> revisions of this file and of the README described a Go implementation; none of
> that code is in this repository, and by the project owner's decision all
> implementation is treated as not started. Update a row to "In progress" or
> "Done" only when its code and tests are committed here.

Legend: **Not started**, **In progress**, **Done**.

The "Planned component" column is a *proposal* for where each requirement would
live (it assumes Go, see open question Q1 in
[10 – Open Questions](10-open-questions.md)); none of these paths exist yet.

## Proposed repository layout

```
cmd/parent-trackerd/       systemd daemon entry point
cmd/parent-tracker/        administration CLI entry point
cmd/parent-tracker-agent/  per-session agent (systemd user unit)
gnome-extension/           GNOME Shell extension (ESM, GNOME 46+) - Wayland foreground sensor
internal/
  config/                  TOML config, defaults, validation
  model/                   domain types
  storage/                 SQLite (WAL), migrations, queries, retention deletes
  collector/               session (logind D-Bus), session-type detection, foreground providers (X11, GNOME-Wayland)
  tracker/                 sampling engine (intervals, samples, gap detection)
  aggregate/               active-time summaries, app aggregates, trends, timeline, hourly buckets
  rollup/                  scheduled aggregation (daily + hourly)
  live/                    in-memory live hub, SSE fan-out
  retention/               scheduled pruning
  alert/                   rule engine + ntfy/webhook/desktop notifiers
  server/                  authenticated dashboard + JSON API
  control/                 local control socket for the child's self-view
  daemon/                  wires everything together, SIGHUP reload
  cli/                     CLI commands
internal/server/web/       dashboard front end (React 18 + Lumen, embedded)
internal/server/web/vendor/ lumen/, react/, fonts/  (vendored, unmodified; see 12)
packaging/                 systemd units, tmpfiles, dconf defaults, .deb build, example config
```

## Functional requirements (docs/02)

| ID | Requirement | Status | Planned component |
| --- | --- | --- | --- |
| FR-1.1 | systemd system service | Not started | `packaging/parent-trackerd.service`, `internal/daemon` |
| FR-1.2 | Survives logout/crash | Not started | daemon + `Restart=on-failure`, orphan-interval recovery |
| FR-1.3 | systemctl control | Not started | systemd unit |
| FR-1.4 | Config in /etc, not world-readable | Not started | `internal/config`, package postinst (0640) |
| FR-1.5 | Data in /var/lib | Not started | `storage.Open`, tmpfiles |
| FR-1.6 | Diagnostic mode | Not started | `parent-tracker doctor` |
| FR-1.7 | Health self-report | Not started | `collector_health` + `/api/health` |
| FR-1.8 | Restart with backoff | Not started | systemd `Restart=on-failure` |
| FR-2.1 | Monitored allowlist | Not started | `config.MonitoredUsers` |
| FR-2.2 | Session start/end | Not started | `sessions` table, logind watcher |
| FR-2.3 | Session states (screen-off is SHOULD) | Not started | `model.SessionState` |
| FR-2.4 | Active time only | Not started | `aggregate` |
| FR-2.5 | Idle via logind | Not started | logind `IdleHint` / `IdleSinceHint` |
| FR-2.6 | Boot/suspend boundaries | Not started | gap detection in `tracker` |
| FR-2.7 | Multiple monitored users | Not started | per-UID/session state |
| FR-2.8 | Remote sessions separated | Not started | logind `Remote` property |
| FR-2.9 | Per-session type detection (Wayland / X11 / tty) | Not started | `collector` via logind `Type` |
| FR-3.1 | X11 foreground window | Not started | X11 provider in the agent (EWMH) |
| FR-3.2 | Wayland (GNOME) foreground app via Shell extension | Not started | `gnome-extension/` + agent relay |
| FR-3.3 | App id/name mapping | Not started | desktop-entry mapping |
| FR-3.4 | Per-app daily aggregation | Not started | `aggregate`, `rollup` |
| FR-3.5 | Window title (optional) | Not started | `collectors.window_title` |
| FR-3.6 | Idle-with-app-open split | Not started | — |
| FR-3.7 | Categories | Not started | `config` |
| FR-3.8 | Full-screen/game foreground | Not started | — |
| FR-3.9 | X11 provider never used on a Wayland session | Not started | provider selection in `collector` |
| FR-3.10 | Core metrics identical on Wayland and X11 | Not started | session collector is session-type independent |
| FR-3.11 | Visible degradation when app tracking unavailable | Not started | `doctor`, `/api/health`, dashboard `InlineAlert` |
| FR-3.12 | Extension installed system-wide, enabled by default, loss logged | Not started | `packaging/` dconf default, heartbeat in agent |
| FR-3.13 | KDE Plasma Wayland foreground (MAY) | Not started | — |
| FR-4.1–4.3 | Web history import (domain default, opt-in) | Not started | `internal/webhist` |
| FR-4.4 | Search-query capture | Not started | — |
| FR-5.x | Screenshots | Not started | — |
| FR-6.1–6.4 | Dashboard daily/weekly/timeline, authenticated, loopback | Not started | `internal/server`, `/api/summary`, `/api/trend`, `/api/timeline` |
| FR-6.5 | CSV/PDF export | Not started | `report`, `export` |
| FR-6.6 | Daily digest | Not started | alert engine |
| FR-6.7 | Budgets | Not started | — |
| FR-6.8–6.16 | Real-time dashboard | Not started | `internal/live`; `GET /api/live`, `GET /api/live/stream` |
| FR-6.20–6.29 | Per-hour history | Not started | `hourly_*` tables, `rollup`, `GET /api/hourly`, `report --by hour` |
| FR-7.1–7.3 | Alerts, channels, rate limiting | Not started | `internal/alert` |
| FR-8.1–8.6 | CLI, reload, safe defaults, doctor, uninstall | Not started | `internal/cli`, SIGHUP reload, package scripts |
| FR-9.x | Enforcement | Not started | deferred (Phase 6) |
| FR-10.1–10.2 | Retention, export/delete | Not started | `internal/retention`, `export`, `delete` |
| FR-10.3 | Child self-view | Not started | `parent-tracker self` over `internal/control` |
| FR-10.4 | Real deletion | Not started | `DeleteUserDataBefore` |
| FR-10.5 | Hourly data in export/delete/self-view | Not started | — |

## Non-functional requirements (docs/03)

| ID | Requirement | Status | Notes |
| --- | --- | --- | --- |
| NFR-1.1–1.7 | CPU, RAM, I/O, interval, page load | Not started | needs measurement once code exists |
| NFR-1.8–1.11 | Live-view cost, bounded clients, hourly query latency | Not started | — |
| NFR-2.x | Durability, isolation, clean restart, clock changes | Not started | — |
| NFR-3.1–3.2 | Ubuntu 24.04+ only; GNOME Shell 46+ | Not started | policy is documented; package gate and `doctor` check not built |
| NFR-3.3, 3.9, 3.10 | Wayland and X11 session support, session-independent core metrics | Not started | see 04 §4.13 |
| NFR-3.4–3.8 | Flavours, arm64, no X11 hard dependency, no legacy shims, early failure on unsupported OS | Not started | — |
| NFR-4.x | Least privilege, file modes, loopback + auth, hardening | Not started | — |
| NFR-5.x | Local-only, no telemetry, allowlist scoping | Not started | — |
| NFR-6.1–6.7 | `.deb`, upgrade safety, migration, config format, CI | Not started | — |
| NFR-6.8–6.11 | Versioned dependencies, hourly migration, amd64 + arm64 packages, third-party notices | Not started | — |
| NFR-7.x | Logs, rotation, `status`, disk guard | Not started | — |
| NFR-8.1–8.4 | Responsive, accessible, non-shaming, localisation | Not started | — |
| NFR-8.5–8.8 | Lumen design system, self-hosted assets, themes, touch | Not started | see doc 12 |
| NFR-9.x | Disclosure, retention docs, child explainer | Not started | — |

## Security, design-system and acceptance requirements

| Area | Requirements | Status |
| --- | --- | --- |
| Security & privacy (docs/06) | SEC-1 … SEC-43 | Not started |
| Design system (docs/12) | DS-1 … DS-14 | Not started |
| Acceptance criteria (docs/08) | AC-1 … AC-117 | Not started (no tests exist) |

## Roadmap phases (docs/09)

| Phase | Status |
| --- | --- |
| 0 Foundations | Not started |
| 1 MVP: session & screen time | Not started |
| 2 Application tracking (Wayland extension, X11 provider) | Not started |
| 3 Reporting, alerts, multi-user polish | Not started |
| 4 Web activity & devices | Not started |
| 5 Hardening & packaging polish | Not started |
| 6 Enforcement | Not started |
| 7 Nice-to-haves | Not started |
| 8 Platform baseline, hourly history, live dashboard | Not started |

## Biggest open design risks

1. **Wayland foreground app** via a GNOME Shell extension (deployment, per-GNOME-
   release compatibility, tamper evidence). Needs a Phase 2 spike on a real
   24.04 Wayland session. See 04 §4.13.
2. **Lumen packaging**: Lumen is a private design-system artifact with no font
   files; a reproducible, offline, vendored snapshot must be produced (Q15).
3. **Idle accuracy**: idle comes from the desktop session's idle hint, so AC-4's
   ±5% depends on the desktop's idle delay and on back-dating to `IdleSinceHint`
   (validate in Phase 1).
4. **Screen-off detection** has no logind signal; it needs a compositor-specific
   source or stays folded into idle/locked (FR-2.3 makes it SHOULD).
