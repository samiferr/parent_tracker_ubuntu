# Parent Tracker — User Guide

A complete guide to installing, configuring and using **Parent Tracker**, a
local service that records how an Ubuntu desktop is used — screen time, idle and
locked time, foreground applications, and (optionally) browser domains.

> **Who this is for.** This guide is written for the parent/administrator who
> owns and administers the PC. Parent Tracker is meant for monitoring your own
> minor child on your own device, openly, not for covert surveillance. See
> [Privacy and legal](#18-privacy-and-legal) at the end.

## Contents

1. [What it does](#1-what-it-does)
2. [Requirements and capabilities](#2-requirements-and-capabilities)
3. [Install](#3-install)
4. [First-time setup](#4-first-time-setup)
5. [Day-to-day service management](#5-day-to-day-service-management)
6. [The dashboard](#6-the-dashboard)
7. [Command-line reference](#7-command-line-reference)
8. [Configuration reference](#8-configuration-reference)
9. [Alerts](#9-alerts)
10. [Browser history](#10-browser-history)
11. [The child's self-view](#11-the-childs-self-view)
12. [Reports and data export](#12-reports-and-data-export)
13. [Data retention, deletion and backups](#13-data-retention-deletion-and-backups)
14. [Customising app categories](#14-customising-app-categories)
15. [Performance and disk usage](#15-performance-and-disk-usage)
16. [Troubleshooting](#16-troubleshooting)
17. [Files and paths](#17-files-and-paths)
18. [Privacy and legal](#18-privacy-and-legal)
19. [Uninstall](#19-uninstall)
20. [FAQ](#20-faq)

---

## 1. What it does

Parent Tracker runs as a background system service and records, for an explicit
**allowlist** of local accounts only:

- **Session activity** — login/logout, active, idle, locked, and screen-off
  time, obtained from `systemd-logind`.
- **Foreground applications** — which app was in front and for how long
  (on X11 out of the box; Wayland degrades, see below).
- **Browser domains** (optional, off by default) — which websites were visited.
- **Tracking gaps** — if the service is stopped or the machine crashes, the gap
  is recorded and shown to you rather than silently hidden.

Everything is stored **locally** in a SQLite database. There is no cloud, no
account, and no telemetry.

It does **not**: take screenshots, read messages, record keystrokes, or filter
network traffic (all deferred or out of scope).

## 2. Requirements and capabilities

- Ubuntu **22.04 LTS** or **24.04 LTS** (or a compatible derivative).
- **systemd** with `systemd-logind` (standard on Ubuntu desktop).
- For application tracking on X11: the `xprop` tool (from `x11-utils`, installed
  automatically as a recommended package).
- The service runs as root (needed to observe other users' sessions and read
  their browser databases) but is heavily sandboxed.

| Capability | X11 session | Wayland session |
| --- | --- | --- |
| Active / idle / locked time | ✅ | ✅ |
| Application (foreground) time | ✅ | ⚠️ Degraded |
| Window titles (optional) | ✅ | ⚠️ Degraded |
| Browser domains (optional) | ✅ | ✅ |

On **Wayland**, GNOME does not let a system service see which window has focus.
Screen time is still recorded, but per-app time is not, and the dashboard shows
a "degraded" banner. A GNOME Shell helper to close this gap is planned but not
yet implemented.

Verify your environment at any time with:

```bash
parent-tracker doctor
```

## 3. Install

### 3.1 From the `.deb` package (recommended)

Build or obtain `parent-tracker_<version>_amd64.deb`, then:

```bash
sudo apt install ./parent-tracker_0.1.1_amd64.deb
```

This installs both binaries, the systemd unit, and a default configuration.
The package:

- creates `/etc/parent-tracker/config.toml` from the shipped example if it does
  not already exist,
- enables (but does not forcibly start) `parent-trackerd.service`,
- creates the data and runtime directories with restrictive permissions.

**Next step:** run the first-time setup (section 4). The service will not
collect anything until you choose which users to monitor.

### 3.2 From source

Requires Go 1.24+ (tested with 1.27):

```bash
git clone <repo> ubuntu_parent_tracker
cd ubuntu_parent_tracker
make build          # -> bin/parent-trackerd, bin/parent-tracker
make test           # optional: run the test suite
```

To produce a package from source:

```bash
make deb            # -> dist/parent-tracker_<version>_amd64.deb
```

To run the binaries directly during development you can point them at a
throwaway config with `--config /path/to/config.toml`; only the `.deb` layout
uses the standard systemd paths.

### 3.3 Verify the install

```bash
parent-tracker version
systemctl status parent-trackerd.service
```

## 4. First-time setup

Run setup once, as root:

```bash
sudo parent-tracker setup
```

It walks you through three things.

### 4.1 Choose monitored users (numbered picker)

Parent Tracker lists the machine's **real user accounts** and filters out
system/service accounts automatically (UID below `UID_MIN`, non-login shells,
`root`, `nobody`, and accounts without a home directory):

```
Local user accounts found (system accounts excluded):
   1) sami                 uid 1000  /home/sami
   2) maram                uid 1001  /home/maram
   3) ilyes                uid 1002  /home/ilyes

Select users to monitor by number (e.g. 1,3): 3
```

Enter one or more numbers separated by commas. Only the accounts you select are
ever recorded.

### 4.2 Set the dashboard login

You are prompted for a username (default `parent`) and a password (minimum 8
characters). The password is stored only as a **bcrypt hash** in the config.

### 4.3 Non-interactive setup

For scripted installs, pass everything as flags:

```bash
sudo parent-tracker setup --non-interactive \
  --user ilyes \
  --dashboard-user parent \
  --dashboard-password 'a-strong-password' \
  --alert-channel ntfy \
  --ntfy-url 'https://ntfy.sh/my-private-topic'
```

When done, start the service:

```bash
sudo systemctl enable --now parent-trackerd
```

Open <http://127.0.0.1:8787> and sign in.

## 5. Day-to-day service management

```bash
sudo systemctl status parent-trackerd      # is it running?
sudo systemctl restart parent-trackerd     # restart
sudo systemctl stop parent-trackerd        # pause tracking
sudo systemctl disable parent-trackerd     # do not start at boot
journalctl -u parent-trackerd -f           # follow the logs
```

### Reloading configuration

Most settings reload without dropping the process:

```bash
sudo systemctl reload parent-trackerd      # sends SIGHUP
```

Reload applies monitored users, collectors, retention and alerts. Changing the
dashboard **listen address** requires a full `restart`.

### Stopping is visible

If you stop the service, Parent Tracker records an audit entry. If
`alerts.notify_on_service_stop` is enabled, an unclean stop (crash, power loss)
produces an alert on the next start so a gap is never silent.

## 6. The dashboard

Open <http://127.0.0.1:8787> and sign in with the credentials from setup.

- **Active time / Idle / Locked** — the day's headline numbers, with a 7-day
  average for context.
- **First / last activity** — when the day's use started and ended.
- **Top applications** — the apps used most in the foreground that day, ranked
  by time.
- **Timeline** — a colour-coded strip of the day: blue = active, amber = idle,
  grey = locked/off. Gaps are visible.
- **Trend** — daily active time over 7 or 30 days.
- **Websites** — top domains over the last 7 days (only if web tracking is
  enabled; otherwise it explains that it is off).
- **Alerts & tracking gaps** — rule hits and any time the service was not
  recording.

Use the **user selector** to switch between monitored accounts and the **date
picker** to look at a specific day.

### Exposing the dashboard on your LAN

By default the dashboard listens on loopback only. To reach it from a phone or
another computer on your home network:

```bash
sudo parent-tracker config set server.listen 0.0.0.0:8787
sudo parent-tracker config set server.allow_lan true
sudo systemctl restart parent-trackerd
```

`allow_lan` must be `true` before a non-loopback address is accepted. Use a
strong password, and consider a firewall rule that restricts the port to your
LAN.

## 7. Command-line reference

All commands accept `-c, --config <path>` (default
`/etc/parent-tracker/config.toml`).

### `parent-tracker version`
Print the version.

### `parent-tracker setup`
Initial configuration. Flags: `--user` (repeatable), `--dashboard-user`,
`--dashboard-password`, `--alert-channel`, `--ntfy-url`, `--non-interactive`.

### `parent-tracker status`
Show config path, data dir, database path, sample interval, monitored users,
schema version, table row counts, and collector health.

### `parent-tracker doctor`
Diagnose everything: config validity, database openability and permissions,
session-provider availability (logind vs loginctl), `xprop` presence, dashboard
configuration. Run this first when something looks wrong.

### `parent-tracker users …`
- `users list` — list monitored users. A `!` marks a configured user that no
  longer exists on the machine.
- `users add <username> [username...]` — start monitoring.
- `users remove <username>` — stop monitoring (existing data is kept).

Changes take effect after `systemctl reload parent-trackerd` (or a restart).

### `parent-tracker config …`
- `config list` — print the full effective configuration as TOML.
- `config get <key>` — print one value.
- `config set <key> <value>` — change and save a value.

Settable keys are listed in [section 8](#8-configuration-reference). Anything
not settable from the CLI can be edited directly in the file.

### `parent-tracker report`
Print a usage report for a monitored user.

```bash
parent-tracker report --user ilyes
parent-tracker report --user ilyes --from 2026-10-01 --to 2026-10-07
parent-tracker report --user ilyes --format csv > usage.csv
```

Flags: `--user`, `--from` (YYYY-MM-DD), `--to`, `--format text|csv`.

### `parent-tracker export`
Dump recorded data as JSON (users, sessions, intervals, app usage, alerts,
audit), with a manifest containing the version and date range.

```bash
parent-tracker export --out backup.json
parent-tracker export --user ilyes --from 2026-10-01 --to 2026-10-07 --out oct.json
parent-tracker export                 # to stdout
```

### `parent-tracker delete`
Permanently delete a user's data older than a date (real `DELETE`, not hidden).

```bash
parent-tracker delete --user ilyes --before 2026-01-01        # asks for confirmation
parent-tracker delete --user ilyes --before 2026-01-01 --yes  # no prompt
```

### `parent-tracker self`
Show **your own** recorded data — the child's read-only view. Requires no root.

```bash
parent-tracker self
parent-tracker self --json
sudo parent-tracker self --user ilyes   # admin: view a specific account
```

See [section 11](#11-the-childs-self-view).

### `parent-tracker serve`
Run the dashboard on its own (no collectors) — useful for debugging.

### `parent-tracker completion`
Generate a shell completion script:

```bash
parent-tracker completion bash | sudo tee /etc/bash_completion.d/parent-tracker
```

## 8. Configuration reference

Configuration lives in `/etc/parent-tracker/config.toml` (mode `0640`). The
annotated example is installed at
`/usr/share/doc/parent-tracker/config.toml.example`.

You can edit the file directly (then `systemctl reload parent-trackerd`), or use
`parent-tracker config set`. Unknown keys are rejected on load.

### General

| Key | Default | Meaning |
| --- | --- | --- |
| `data_dir` | `/var/lib/parent-tracker` | Where the database lives. |
| `db_name` | `tracker.db` | Database file name inside `data_dir`. |
| `log_level` | `info` | `debug`, `info`, `warn`, `error`. |
| `sample_interval` | `5s` | How often the machine is sampled (1s–1m). |
| `idle_threshold` | `5m` | Retained for compatibility; idle is taken from logind. |
| `web_import_interval` | `15m` | How often browser history is imported. |
| `control_socket` | `/run/parent-tracker/control.sock` | Socket for the child self-view. |
| `monitored_users` | `[]` | The allowlist. Only these accounts are recorded. |

### `[collectors]` — what to collect

| Key | Default | Meaning |
| --- | --- | --- |
| `session` | `true` | Login/active/idle/locked tracking. |
| `app` | `true` | Foreground application tracking. |
| `window_title` | `false` | Store window titles (privacy-sensitive). |
| `web` | `false` | Import browser history. |
| `web_full_url` | `false` | Store full URLs + page titles instead of domain-only. Requires `web`. |
| `screenshots` | `false` | Reserved; not implemented. |
| `devices` | `false` | Reserved; not implemented. |

### `[retention]` — how long to keep data

| Key | Default | Meaning |
| --- | --- | --- |
| `window_samples` | `720h` (30d) | Raw app samples. |
| `intervals` | `4320h` (180d) | Activity intervals. |
| `app_usage` | `17520h` (730d) | Per-app daily aggregates. |
| `web_visits` | `2160h` (90d) | Browser visits. |
| `alerts` | `8760h` (365d) | Alerts. |
| `sessions` | `8760h` (365d) | Login sessions. |
| `prune_interval` | `6h` | How often the pruner runs. |

### `[server]` — the dashboard

| Key | Default | Meaning |
| --- | --- | --- |
| `enabled` | `true` | Serve the dashboard. |
| `listen` | `127.0.0.1:8787` | Bind address. |
| `username` | `parent` | Dashboard login. |
| `password_hash` | — | bcrypt hash (set by `setup`). |
| `allow_lan` | `false` | Must be `true` to bind beyond loopback. |

### `[alerts]`

| Key | Default | Meaning |
| --- | --- | --- |
| `enabled` | `false` | Turn the alert engine on. |
| `channel` | `none` | `none`, `desktop`, `ntfy`, `webhook`. |
| `ntfy_url` | — | ntfy endpoint (with `channel = "ntfy"`). |
| `webhook_url` | — | JSON POST endpoint (with `channel = "webhook"`). |
| `daily_active_limit` | `0s` | Alert when a day's active time exceeds this (`0s` disables). |
| `bedtime` | `""` | Local `HH:MM`; usage after it triggers an alert. Empty disables. |
| `notify_on_service_stop` | `true` | Alert on gaps / unclean stops. |

### `[[categories]]`
Maps applications to human categories using glob patterns. Defaults cover games,
school, social, video and browser. See
[section 14](#14-customising-app-categories).

### Setting values from the CLI

Settable keys: `log_level`, `sample_interval`, `idle_threshold`,
`web_import_interval`, `control_socket`, `server.enabled`, `server.listen`,
`server.allow_lan`, `collectors.session`, `collectors.app`,
`collectors.window_title`, `collectors.web`, `collectors.web_full_url`,
`collectors.screenshots`, `alerts.enabled`, `alerts.channel`, `alerts.ntfy_url`,
`alerts.webhook_url`, `alerts.daily_active_limit`, `alerts.bedtime`,
`retention.window_samples`, `retention.intervals`, `retention.app_usage`,
`retention.prune_interval`.

```bash
sudo parent-tracker config set sample_interval 10s
sudo parent-tracker config set collectors.window_title true
sudo parent-tracker config set alerts.daily_active_limit 3h
```

Values that are not settable from the CLI (for example `retention.web_visits`)
can be edited in the file.

## 9. Alerts

Alerts currently cover:

- **Daily active-time limit** — fires once per user per day.
- **Usage after bedtime** — fires once per user per day.
- **Tracking interrupted** — a gap or unclean stop.

Enable the channel you want:

```bash
# Local desktop notification (uses notify-send)
sudo parent-tracker config set alerts.enabled true
sudo parent-tracker config set alerts.channel desktop

# ntfy (simple, works on your phone)
sudo parent-tracker config set alerts.channel ntfy
sudo parent-tracker config set alerts.ntfy_url https://ntfy.sh/my-private-topic

# Generic webhook (JSON POST)
sudo parent-tracker config set alerts.channel webhook
sudo parent-tracker config set alerts.webhook_url https://example.internal/hook
```

Then set rules:

```bash
sudo parent-tracker config set alerts.daily_active_limit 3h
sudo parent-tracker config set alerts.bedtime 21:00
sudo systemctl reload parent-trackerd
```

Alerts are rate-limited (at most one per rule per user per day) and recorded in
the **Alerts & tracking gaps** panel regardless of delivery success.

## 10. Browser history

Web history is **off by default** because it is more sensitive than screen time.

```bash
sudo parent-tracker config set collectors.web true
sudo systemctl reload parent-trackerd
```

Parent Tracker reads the browser's own visit database — it never decrypts
traffic. Supported: **Firefox** and the **Chromium family** (Chrome, Chromium,
Brave, Edge), including snap and Flatpak locations.

By default only the **domain** is stored (`youtube.com`). To store full URLs and
page titles as well:

```bash
sudo parent-tracker config set collectors.web_full_url true
```

> Full URLs and titles are far more revealing. Leave this off unless you have a
> specific reason and the child knows.

Imports run every `web_import_interval` (default 15 minutes) and are
incremental — each run only picks up visits newer than the last import, so it is
cheap even on a large history.

The dashboard's **Websites** panel shows the top domains over the last 7 days.
Because the service runs as root, it can read browser databases in a monitored
user's home; other accounts are never touched.

## 11. The child's self-view

Part of the design is that the monitored child can see exactly what is recorded
about them. That is what `parent-tracker self` provides.

The child runs it **without root**:

```bash
parent-tracker self
```

```
Recorded activity for ilyes — 2026-10-09

  Active time:      1h 12m
  Idle time:        23m
  Locked / off:     34m
  First activity:   16:05
  Last activity:    20:41

  Applications (last 7 days):
    firefox                   48m
    code                      21m

  Websites (last 7 days):
    youtube.com               12 visit(s)

This is everything Parent Tracker records about you. It stays on this computer.
```

**How it works without giving the child access to everyone's data:** the daemon
serves a local Unix socket (`control_socket`). The caller is identified by the
kernel via `SO_PEERCRED`, and the service returns only that caller's own records.
A non-root user cannot request another user's data. Root/admin may use
`--user <name>`.

Add `--json` for scripted access.

## 12. Reports and data export

Text report for the last 7 days (default):

```bash
parent-tracker report --user ilyes
```

A specific range:

```bash
parent-tracker report --user ilyes --from 2026-10-01 --to 2026-10-07
```

CSV (good for a spreadsheet):

```bash
parent-tracker report --user ilyes --format csv > ilyes-october.csv
```

Full JSON export (everything, with a manifest):

```bash
parent-tracker export --out tracker-backup.json
```

## 13. Data retention, deletion and backups

**Retention** keeps the database bounded. Raw samples expire after
`retention.window_samples` (default 30 days) and are removed, but the per-app
daily aggregates are kept much longer (default 2 years), so old trends remain
visible. The pruner runs every `retention.prune_interval` (default 6h).

**Deleting data** is a real, irreversible delete:

```bash
parent-tracker delete --user ilyes --before 2026-01-01 --yes
```

**Backups:** the database is a single SQLite file. Back it up safely with the
CLI export:

```bash
parent-tracker export --out ~/tracker-backup-$(date +%F).json
```

Or copy the SQLite file while the service is stopped, or with the SQLite backup
API. The database uses WAL mode, so a plain copy of `tracker.db` while running
may miss recent writes in the `-wal` file.

## 14. Customising app categories

Categories group applications in reports and the dashboard. They are glob
patterns matched against the application id (lower-cased). Edit the
`[[categories]]` tables in `/etc/parent-tracker/config.toml`:

```toml
[[categories]]
name = "games"
patterns = ["steam", "*game*", "minecraft", "lutris"]

[[categories]]
name = "school"
patterns = ["libreoffice*", "code", "zotero", "anki"]
```

An application that matches no category is labelled `other`. Reload after
editing.

## 15. Performance and disk usage

Parent Tracker is designed to be lightweight:

- one sampling ticker (default every 5 seconds), batched database writes,
- steady-state CPU well under 1% of a core and memory well under 100 MB,
- disk growth is bounded by retention. User-facing dashboard queries read
  pre-aggregated daily tables.

Approximate raw storage at default settings is a few megabytes per user per
month, pruned to 30 days.

## 16. Troubleshooting

**Always start here:**

```bash
parent-tracker doctor
journalctl -u parent-trackerd -n 100 --no-pager
```

| Symptom | Likely cause and fix |
| --- | --- |
| Dashboard says "login is not configured" | Run `sudo parent-tracker setup` (or the service before setup). |
| Dashboard unreachable from another device | Loopback-only by default; set `server.listen` and `server.allow_lan true`, then restart. |
| No application data | Wayland session (degraded — expected), or no `xprop` installed, or the session is a TTY. Check `parent-tracker doctor`. |
| No web data | `collectors.web` is off (default), or the browser hasn't been used. |
| `status` shows collectors `DEGRADED` | Informational on Wayland; see above. |
| Service won't start | `/etc/parent-tracker/config.toml` must exist (the unit has `ConditionPathExists`). Run `setup`. |
| "database … too open" in doctor | Fix with `sudo chmod 0640 /var/lib/parent-tracker/tracker.db`. |
| Times look shifted | Day boundaries use the service's local timezone. Set the system timezone correctly. |
| Config edit rejected | Unknown keys are errors. Run `parent-tracker config list` to see valid keys. |
| A monitored user vanished from the list | Their account was removed; `users list` marks stale entries with `!`. |

**Reset the dashboard password:**

```bash
sudo parent-tracker setup --non-interactive --dashboard-user parent --dashboard-password 'new-password'
sudo systemctl reload parent-trackerd
```

## 17. Files and paths

| Path | Purpose |
| --- | --- |
| `/usr/bin/parent-trackerd` | The service binary. |
| `/usr/bin/parent-tracker` | The admin CLI. |
| `/etc/parent-tracker/config.toml` | Configuration (`0640`). |
| `/var/lib/parent-tracker/tracker.db` | SQLite database (`0600`), plus `-wal`/`-shm`. |
| `/run/parent-tracker/control.sock` | Child self-view socket. |
| `/usr/lib/systemd/system/parent-trackerd.service` | systemd unit. |
| `/usr/lib/tmpfiles.d/parent-tracker.conf` | Runtime directory definitions. |
| `/usr/share/doc/parent-tracker/` | Example config and docs. |
| `journalctl -u parent-trackerd` | Logs. |

## 18. Privacy and legal

- Parent Tracker is intended for a parent or guardian monitoring their **own
  minor child** on a device they own and administer.
- The design is **transparent**: the child can see everything recorded about
  them with `parent-tracker self`. There is no hidden or covert mode.
- Do **not** use it to monitor adults, guests, or devices you do not control.
- Data stays on the machine. There is no telemetry and no cloud. The only
  outbound network traffic is to an alert channel you explicitly configure.
- Sensitive collection (window titles, browser history, full URLs) is off by
  default. Keep retention as short as is useful.
- Laws differ by jurisdiction; parental monitoring of a minor on your own device
  is generally permitted, but you are responsible for complying with local law
  and for treating the child's data with care.

## 19. Uninstall

Remove the program but **keep** recorded data:

```bash
sudo apt remove parent-tracker
```

Remove everything, **including data and configuration**:

```bash
sudo apt purge parent-tracker
```

`apt purge` deletes `/var/lib/parent-tracker` and `/etc/parent-tracker`. Export
first if you want to keep a copy:

```bash
parent-tracker export --out ~/tracker-backup.json
```

## 20. FAQ

**Does it work while the child is logged out?**
Yes. The service runs as a system service independent of any login, and records
logout/lock times.

**Can I monitor more than one child?**
Yes. `users add` accepts multiple names, and it is fine to monitor several
accounts; the dashboard has a user selector.

**Will the child notice the service?**
It runs as a normal systemd service. Per the project's design it is not hidden —
tell the child it is running and show them `parent-tracker self`.

**Does it slow the PC down?**
No. It samples every few seconds and batches writes; see
[section 15](#15-performance-and-disk-usage).

**What happens if the PC is suspended or crashes?**
The gap is detected and recorded, and shown as a tracking gap; it is never
counted as usage. With `notify_on_service_stop`, you get an alert.

**Can I see full URLs?**
Only if you enable `collectors.web_full_url`. Otherwise only domains are stored.

**How do I move the data to a new PC?**
`parent-tracker export` on the old machine, then reinstall and set up on the
new one; the export is JSON for archival/reference (there is no import command
yet).
