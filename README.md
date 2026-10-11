# Ubuntu Parent Tracker

A small, self-hosted service for Ubuntu desktops that helps a parent understand
how their **child's** PC is used: active screen time, idle vs. locked time, and
which applications were in the foreground. Everything stays on the machine, and
the child can be shown exactly what is recorded.

> **Intended use:** a parent monitoring their own minor child on a computer they
> own and administer. It is **not** covert surveillance and must not be used to
> monitor adults or devices you do not control. See
> [docs/06-security-privacy-and-legal.md](docs/06-security-privacy-and-legal.md).

## Status: specification only — no code yet

**This repository currently contains the specification and documentation only.
No code has been written; every implementation item is "Not started".** The
commands, configuration keys and screens described below and in the
[User Guide](docs/USER-GUIDE.md) are the *planned* interface, not something you
can install today. Progress is tracked requirement by requirement in
[docs/11-implementation-status.md](docs/11-implementation-status.md) and
phase by phase in the [roadmap](docs/09-roadmap.md).

## What it is designed to do

- A systemd service (`parent-trackerd`) with sandboxing and clean shutdown.
- Session, idle and lock tracking via **systemd-logind**, identical on Wayland
  and X11 sessions.
- Foreground application tracking on **both session types**: an EWMH provider on
  X11 sessions and a **GNOME Shell extension** on Wayland sessions (see
  [Supported platform](#supported-platform)).
- SQLite (WAL) storage with retention, aggregation and safe migrations.
- A **real-time dashboard**: a live "Now" card (state, foreground app, running
  totals) that updates by itself over Server-Sent Events, with a polling
  fallback and a clear Live / Stale indicator. Status only — never screen
  content. See [04 §4.11](docs/04-architecture.md#411-live-pipeline-real-time-dashboard).
- **Per-hour history**: usage by local hour for any day, a day × hour heatmap
  for 7/30 days, per-hour top apps, and `parent-tracker report --by hour`. Kept
  for 1 year by default. See [04 §4.12](docs/04-architecture.md#412-hourly-rollup).
- A **child self-view** — `parent-tracker self` shows a monitored user their own
  data (including the hourly breakdown) without root.
- Optional browser-history import (domain-only by default) and alerts via
  ntfy / webhook / desktop.
- An authenticated, local web dashboard built with the **Lumen design system**
  ([docs/12](docs/12-design-system-lumen.md)), fully self-contained (no CDN, no
  web fonts, no telemetry).

Deliberately later or out of scope: screenshots, search-term capture,
enforcement (time limits), mobile apps.

## Supported platform

**Ubuntu 24.04 LTS or newer — only.** Ubuntu 22.04 LTS and older are not
supported and are not tested. The baseline is systemd 255, glibc 2.39 and
GNOME Shell 46. The `.deb` is specified to refuse installation on older releases
(versioned dependencies, NFR-6.8); on a 22.04 machine, upgrade Ubuntu to 24.04
first. Details: [docs/03 §3.3](docs/03-non-functional-requirements.md#33-compatibility),
[docs/04 §4.10](docs/04-architecture.md#410-platform-baseline-ubuntu-2404-and-newer).
Packages are planned for **amd64 and arm64**.

**Both Wayland and X11 sessions are required**, and neither can be dropped:

| Release | GNOME sessions offered | What the tracker needs |
| --- | --- | --- |
| Ubuntu 24.04 LTS | Wayland (default) **and** X11 ("Ubuntu on Xorg") | Both providers |
| Ubuntu 25.10 and later | **Wayland only** (X11 apps run via XWayland) | The Wayland provider |
| X11-only flavours (e.g. Xubuntu, Ubuntu MATE) | X11 | The X11 provider, basic support |

Screen time comes from systemd-logind and is the same on every session type.
Foreground-app data needs the X11 provider on X11 sessions and the GNOME Shell
extension on Wayland sessions (GNOME does not let a root daemon read other
windows). Without the right provider the dashboard says so and screen time keeps
being recorded. See [docs/04 §4.13](docs/04-architecture.md#413-session-types-wayland-and-x11).

## Architecture in one picture

```
 parent-trackerd (systemd, root, hardened)
 ├─ session collector ──► systemd-logind (system bus): state, idle, lock, session type
 ├─ tracker loop ◄─────── unix socket ◄── parent-tracker-agent (one per session, runs as the user)
 │                                        ├─ X11 provider (EWMH)             [x11 sessions]
 │                                        └─ GNOME Shell extension relay     [wayland sessions]
 ├─ rollup / retention ► daily + hourly aggregates ─┐
 ├─ alert engine ──────► ntfy / webhook / desktop   ├─► SQLite (WAL)  /var/lib/parent-tracker
 ├─ live hub ──────────► in-memory state → SSE      │
 └─ dashboard ─────────► 127.0.0.1:8787 (auth) ─────┘   React 18 + Lumen, embedded assets
```

Everything in this picture is planned. See [docs/04-architecture.md](docs/04-architecture.md).

## Planned stack and build

The proposed layout and build commands assume **Go** (open question Q1 in
[docs/10](docs/10-open-questions.md) — not yet confirmed). Nothing below exists
yet.

```bash
make build            # -> bin/parent-trackerd, bin/parent-tracker, bin/parent-tracker-agent
make test
make deb              # -> dist/parent-tracker_<version>_<arch>.deb  (amd64 and arm64)
```

## Planned install and administration

```bash
sudo apt install ./parent-tracker_<version>_<arch>.deb
sudo parent-tracker setup            # pick monitored users + dashboard login + acknowledge the disclosure
sudo systemctl enable --now parent-trackerd
parent-tracker doctor                # OS support, session type, extension, permissions
```

On a **Wayland** session the monitored user must log out and back in once after
installation so GNOME Shell loads the extension; `doctor` verifies it. Then open
<http://127.0.0.1:8787>.

```bash
parent-tracker status                # service, storage, collector health, session types
parent-tracker users add alice       # start monitoring a user
parent-tracker users remove alice    # stop monitoring (data retained)
parent-tracker config set sample_interval 5s
parent-tracker config set alerts.channel ntfy
parent-tracker report --user alice --from 2026-10-01 --to 2026-10-07
parent-tracker report --user alice --by hour   # per-hour table
parent-tracker self                  # what is recorded about *you* (no root)
parent-tracker export --out backup.json
parent-tracker delete --user alice --before 2026-01-01 --yes
```

Apply most changes without a restart with `sudo systemctl reload parent-trackerd`
(SIGHUP). Changing the dashboard listen address needs a full restart.

## Planned configuration

`/etc/parent-tracker/config.toml` (mode 0640). Only users listed in
`monitored_users` are ever recorded. Sensitive collectors are off by default:

```toml
monitored_users = ["alice"]

[collectors]
session = true
app = true
window_title = false   # privacy-sensitive: off by default
web = false            # browser history import; off by default
web_full_url = false   # store full URLs + titles instead of domain-only
screenshots = false    # reserved; not planned for the first release

[retention]
window_samples = "720h"  # 30 days
app_usage = "17520h"     # 2 years
hourly_usage = "8760h"   # 1 year of per-hour history

[server]
listen = "127.0.0.1:8787"
allow_lan = false        # LAN exposure is opt-in and warned about
live = true              # real-time dashboard updates
live_max_clients = 10    # cap on simultaneous live viewers

[alerts]
enabled = false
channel = "none"         # none | desktop | ntfy | webhook
daily_active_limit = "0s"
bedtime = ""
```

A fully annotated example will live at `packaging/config.toml.example`.

## Privacy and safety (design commitments)

- Data stays local; there is no telemetry and no cloud — including in the
  dashboard, which loads nothing from other origins.
- Tracking is scoped to an explicit allowlist.
- The database is owner-only (`0600`); the config is `0640`.
- The dashboard binds to loopback and requires a password; LAN access is opt-in.
- Stopping the service, losing the app sensor, or hitting a tracking gap is
  recorded and surfaced. The app-level data comes from the monitored user's own
  session, so a determined user can interfere with it: the design provides
  evidence, not enforcement ([SEC-35](docs/06-security-privacy-and-legal.md)).
- The live view shows status only — never screen content — and the child's
  `parent-tracker self` states that it exists and shows the same per-hour history
  the parent sees.
- `apt purge` deletes recorded data; `apt remove` keeps it.

## Documentation

| Doc | Contents |
| --- | --- |
| [User Guide](docs/USER-GUIDE.md) | How the finished product is meant to be installed, configured and used |
| [01 – Overview & Scope](docs/01-overview.md) | Problem, goals, personas, scope |
| [02 – Functional Requirements](docs/02-functional-requirements.md) | What the system must do |
| [03 – Non-Functional Requirements](docs/03-non-functional-requirements.md) | Performance, security, compatibility |
| [04 – Architecture](docs/04-architecture.md) | Components, data flow, live pipeline, hourly rollup, Wayland/X11 session types |
| [05 – Data Model](docs/05-data-model.md) | Entities, retention, schemas |
| [06 – Security, Privacy & Legal](docs/06-security-privacy-and-legal.md) | Consent, tamper evidence, threat model |
| [07 – UX & Reporting](docs/07-ux-reporting.md) | Dashboard, reports, alerts, CLI |
| [08 – Testing & Acceptance](docs/08-testing-acceptance.md) | Acceptance criteria and test plan |
| [09 – Roadmap](docs/09-roadmap.md) | Phased delivery |
| [10 – Open Questions & Assumptions](docs/10-open-questions.md) | Decisions and defaults |
| [11 – Implementation Status](docs/11-implementation-status.md) | Requirement-to-planned-component map; all Not started |
| [12 – Design System: Lumen](docs/12-design-system-lumen.md) | The UI requirement: Lumen tokens and components |

## License

Private family project; no license chosen yet.
