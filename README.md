# Ubuntu Parent Tracker

A small, self-hosted service for Ubuntu desktops that helps a parent understand
how their **child's** PC is used: active screen time, idle vs. locked time, and
which applications were in the foreground. Everything stays on the machine, and
the child can be shown exactly what is recorded.

> **Intended use:** a parent monitoring their own minor child on a computer they
> own and administer. It is **not** covert surveillance and must not be used to
> monitor adults or devices you do not control. See
> [docs/06-security-privacy-and-legal.md](docs/06-security-privacy-and-legal.md).

📖 **New here? Read the [User Guide](docs/USER-GUIDE.md)** — installation,
setup, configuration, every command, alerts, web history, the child's
self-view, and troubleshooting.

## Status

An MVP is implemented in Go and builds/tests cleanly on Linux (Go 1.24+):

- systemd service (`parent-trackerd`) with sandboxing and clean shutdown,
- session/idle/lock tracking via **systemd-logind** (D-Bus, with a `loginctl`
  fallback),
- foreground application tracking on **X11** via EWMH/`xprop`; **Wayland**
  degrades gracefully (screen time still recorded, per-app time pending a GNOME
  helper),
- SQLite (WAL) storage with retention, aggregation and safe migrations,
- **browser history import** (Firefox + Chromium-family), domain-only by default,
- authenticated local **web dashboard** + JSON API,
- **child self-view** — `parent-tracker self` shows a monitored user their own
  data without root, over a peer-credential-checked local socket,
- alerts via **ntfy / webhook / desktop**, rate-limited,
- admin **CLI** with a numbered user picker in `setup`, `.deb` packaging, and an
  implementation status map in
  [docs/11-implementation-status.md](docs/11-implementation-status.md).

Not yet built: Wayland app tracking, screenshots, search-term capture, and
enforcement (time limits). These are deliberately phased.

## Architecture in one picture

```
        parent-trackerd (systemd, root, hardened)
        ├─ session collector ──► systemd-logind (D-Bus)   ─┐
        ├─ app collector ─────► xprop / EWMH (X11)          │
        ├─ tracker loop ──────► activity intervals + samples│
        ├─ rollup / retention ► per-app daily aggregates    ├─► SQLite (WAL)
        ├─ alert engine ──────► ntfy / webhook / desktop    │   /var/lib/parent-tracker
        └─ dashboard (chi) ───► 127.0.0.1:8787 (auth)      ─┘
```

A single always-on daemon owns storage and logic. Application-level collection
on Wayland will use a per-session helper in a later phase.

## Build from source

Requires Go 1.24+ (tested with 1.27) and, for packaging, `dpkg-deb`.

```bash
make build            # -> bin/parent-trackerd, bin/parent-tracker
make test             # unit + integration tests
make vet
make deb              # -> dist/parent-tracker_<version>_amd64.deb
```

## Install

```bash
sudo apt install ./dist/parent-tracker_0.1.0_amd64.deb
sudo parent-tracker setup            # pick monitored users by number + set a password
sudo systemctl enable --now parent-trackerd
```

`setup` lists the machine's real user accounts (system/service accounts are
filtered out) and lets you choose which to track by number. Then open
<http://127.0.0.1:8787> and sign in.

## Administer

```bash
parent-tracker status                # service, storage and collector health
parent-tracker doctor                # diagnose config, permissions, capabilities
parent-tracker users add alice       # start monitoring a user
parent-tracker users remove alice    # stop monitoring (data retained)
parent-tracker config set sample_interval 5s
parent-tracker config set alerts.enabled true
parent-tracker config set alerts.channel ntfy
parent-tracker config set alerts.ntfy_url https://ntfy.sh/my-topic
parent-tracker report --user alice --from 2026-10-01 --to 2026-10-07
parent-tracker report --user alice --format csv
parent-tracker self                  # what is recorded about *you* (no root)
parent-tracker export --out backup.json
parent-tracker delete --user alice --before 2026-01-01 --yes
```

Apply most changes without a restart with `sudo systemctl reload parent-trackerd`
(SIGHUP). Changing the dashboard listen address needs a full restart.

## Configuration

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
screenshots = false    # not implemented yet

[retention]
window_samples = "720h"  # 30 days
app_usage = "17520h"     # 2 years

[server]
listen = "127.0.0.1:8787"
allow_lan = false        # LAN exposure is opt-in and warned about

[alerts]
enabled = false
channel = "none"         # none | desktop | ntfy | webhook
daily_active_limit = "0s"
bedtime = ""
```

The full annotated example lives at
[`packaging/config.toml.example`](packaging/config.toml.example).

## Privacy and safety

- Data stays local; there is no telemetry and no cloud.
- Tracking is scoped to an explicit allowlist.
- The database is owner-only (`0600`); the config is `0640`.
- The dashboard binds to loopback and requires a password; LAN access is opt-in.
- Stopping the service or hitting a tracking gap is recorded and surfaced.
- `apt purge` deletes recorded data; `apt remove` keeps it.

## Requirements documentation

The design docs that this implementation follows:

| Doc | Contents |
| --- | --- |
| [01 – Overview & Scope](docs/01-overview.md) | Problem, goals, personas, scope |
| [02 – Functional Requirements](docs/02-functional-requirements.md) | What the system must do |
| [03 – Non-Functional Requirements](docs/03-non-functional-requirements.md) | Performance, security, compatibility |
| [04 – Architecture](docs/04-architecture.md) | Components, data flow, collection mechanisms |
| [05 – Data Model](docs/05-data-model.md) | Entities, retention, schemas |
| [06 – Security, Privacy & Legal](docs/06-security-privacy-and-legal.md) | Consent, tamper evidence, threat model |
| [07 – UX & Reporting](docs/07-ux-reporting.md) | Dashboard, reports, alerts, CLI |
| [08 – Testing & Acceptance](docs/08-testing-acceptance.md) | Acceptance criteria and test plan |
| [09 – Roadmap](docs/09-roadmap.md) | Phased delivery |
| [10 – Open Questions & Assumptions](docs/10-open-questions.md) | Decisions and defaults |
| [11 – Implementation Status](docs/11-implementation-status.md) | Requirement-to-code mapping |
| [User Guide](docs/USER-GUIDE.md) | Install, configure and use every feature |

## License

Private family project; no license chosen yet.
