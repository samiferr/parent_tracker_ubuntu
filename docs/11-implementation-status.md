# 11 – Implementation Status

Maps the requirements in docs/02 and docs/03 to the Go implementation this repo
currently contains. Legend: **Done**, **Partial**, **Not started**.

## Repository layout

```
cmd/parent-trackerd/       systemd daemon entry point
cmd/parent-tracker/        administration CLI entry point
internal/
  config/                  TOML config, defaults, validation
  model/                   domain types
  storage/                 SQLite (WAL), migrations, queries, retention deletes
  collector/               session (logind D-Bus + loginctl) and app (X11/Wayland) collectors
  tracker/                 sampling engine (intervals, samples, gap detection)
  aggregate/               active-time summaries, app aggregates, trends, timeline
  rollup/                  scheduled aggregation of samples into app_usage
  retention/               scheduled pruning
  alert/                   rule engine + ntfy/webhook/desktop notifiers
  server/                  authenticated dashboard + JSON API (chi)
  daemon/                  wires everything together, SIGHUP reload
  cli/                     cobra commands
  logx/, version/          helpers
internal/server/web/       embedded dashboard (HTML/CSS/JS)
packaging/                 systemd unit, tmpfiles, .deb build, example config
```

## Functional requirements

| ID | Requirement | Status | Where |
| --- | --- | --- | --- |
| FR-1.1 | systemd system service | Done | `packaging/parent-trackerd.service`, `internal/daemon` |
| FR-1.2 | Survives logout/crash | Done | daemon + `Restart=on-failure`, orphan-interval recovery |
| FR-1.3 | systemctl control | Done | systemd unit |
| FR-1.4 | Config in /etc, not world-readable | Done | `internal/config`, `.deb` postinst (0640) |
| FR-1.5 | Data in /var/lib | Done | `storage.Open`, tmpfiles |
| FR-1.6 | Diagnostic mode | Partial | `parent-tracker doctor` |
| FR-1.7 | Health self-report | Done | `collector_health` table + `/api/health` |
| FR-1.8 | Restart with backoff | Done | systemd `Restart=on-failure`, `RestartSec=5` |
| FR-2.1 | Monitored allowlist | Done | `config.MonitoredUsers`, `IsMonitored` |
| FR-2.2 | Session start/end | Done | `sessions` table + `UpsertSession/CloseSession` |
| FR-2.3 | Session states | Done (screen-off not distinguished) | `model.SessionState`, `deriveState` |
| FR-2.4 | Active time only | Done | `aggregate.Summarize` |
| FR-2.5 | Idle via logind | Done | `Session.IdleHint` |
| FR-2.6 | Boot/suspend boundaries | Partial | tracking-gap detection in `tracker.sample` |
| FR-2.7 | Multiple monitored users | Done | per-UID open-interval map |
| FR-2.8 | Remote sessions separated | Partial | `Remote` flag recorded, excluded from usage |
| FR-3.1 | X11 foreground window | Done | `collector.X11Provider` (xprop/EWMH) |
| FR-3.2 | Wayland foreground app | Not started | `collector.WaylandProvider` degrades; GNOME helper pending |
| FR-3.3 | App id/name mapping | Done | `parseXpropWindow`, `/proc/<pid>/exe` fallback |
| FR-3.4 | Per-app daily aggregation | Done | `aggregate.AppUsageFromSamples`, `rollup` |
| FR-3.5 | Window title (optional) | Done | `collectors.window_title` |
| FR-3.6 | Idle-with-app-open split | Partial | idle excluded; open_ms reserved |
| FR-3.7 | Categories | Done | `config.CategoryFor` |
| FR-4.1 | Web history import | Done | Firefox `places.sqlite` + Chromium-family `History` (copied, read-only); `internal/webhist` |
| FR-4.2 | Domain vs full URL | Done | domain-only default; `collectors.web_full_url` opt-in |
| FR-4.3 | Switchable, off by default | Done | `collectors.web` default false |
| FR-4.4 | Search-query capture | Not started | — |
| FR-5.x | Screenshots | Not started | off by default |
| FR-6.1/6.2 | Dashboard daily/weekly | Done | `server/web`, `/api/summary`, `/api/trend` |
| FR-6.3 | Timeline | Done | `/api/timeline`, `aggregate.Timeline` |
| FR-6.4 | Authenticated, loopback | Done | `internal/server/auth.go`, config validation |
| FR-6.5 | CSV/PDF export | Partial | CSV via `report`; JSON via `export`; PDF not started |
| FR-6.6 | Daily digest | Partial | alert engine can fire; scheduled digest not built |
| FR-7.1/7.2 | Alerts + channels | Done (ntfy/webhook/desktop) | `internal/alert` |
| FR-7.3 | Rate limiting | Done | `fireOnceToday`, `CountRecentAlerts` |
| FR-8.1 | CLI | Done | `internal/cli` |
| FR-8.2 | Reload without reinstall | Done | SIGHUP → `Daemon.Reload` |
| FR-8.3 | Dashboard settings UI | Partial | `/api/config` read-only |
| FR-8.4 | Safe defaults | Done | `config.Default` |
| FR-8.5 | doctor | Done | `internal/cli/status.go` |
| FR-8.6 | Uninstall/purge | Done | `packaging/debian/postrm` |
| FR-9.x | Enforcement | Not started | deliberately deferred |
| FR-10.1 | Retention | Done | `internal/retention` |
| FR-10.2 | Export/delete | Done | `export`, `delete` CLI |
| FR-10.3 | Child self-view | Done | `parent-tracker self` over the local control socket (`internal/control`) |
| FR-10.4 | Real deletion | Done | `DeleteUserDataBefore` |

## Non-functional requirements

| ID | Requirement | Status | Notes |
| --- | --- | --- | --- |
| NFR-1.1/1.2 | Low CPU/RAM | Likely | single ticker, batched writes; needs real-hardware measurement |
| NFR-2.1 | No data loss | Done | WAL + orphan-interval recovery |
| NFR-2.2 | Collector isolation | Done | errors logged per collector; health recorded |
| NFR-2.3 | Clean restart | Done | `CloseOrphanIntervals` |
| NFR-2.4 | Clock changes | Done | gaps ignored, negative durations clamped |
| NFR-3.1/3.3 | 22.04/24.04, X11+Wayland | Partial | X11 full; Wayland degrades app tracking |
| NFR-4.1/4.2 | Least privilege, file modes | Done | root + sandbox; db 0600, config 0640 |
| NFR-4.3 | Loopback + auth | Done | enforced by config validation + auth |
| NFR-4.6 | systemd hardening | Done | directives in unit file |
| NFR-5.1/5.2 | Local-only, no telemetry | Done | no outbound calls except configured alerts |
| NFR-5.3 | Allowlist scoping | Done | non-monitored sessions never persisted |
| NFR-6.1 | .deb packaging | Done | `packaging/build-deb.sh` |
| NFR-6.2 | Non-clobbering upgrade | Done | config generated only if absent |
| NFR-6.4 | Schema migration + backup | Partial | migrations yes; automatic pre-upgrade backup TODO |
| NFR-7.1 | Structured logs | Done | log/slog to stderr/journald |

## Deliberate gaps / next work

1. **Wayland app tracking** (the biggest gap). Needs a GNOME Shell extension or
   supported D-Bus API; until then Wayland sessions record screen time but not
   per-app time, and the dashboard shows a degraded banner.
2. **Enforcement** — time limits, bedtimes and blocking are intentionally
   deferred to a later phase.
3. **Screen-off detection** — currently folds into idle/locked.
4. **Search-term capture** (FR-4.4) — intentionally not built.
5. **Automatic backup before migration** and **encryption at rest**.
6. **Dashboard settings UI** — config is read-only in `/api/config`; editing is
   via the CLI for now.
