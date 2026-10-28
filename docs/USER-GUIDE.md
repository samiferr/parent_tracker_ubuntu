# Parent Tracker — User Guide

A complete guide to installing, configuring and using **Parent Tracker**, a
local service that records how an Ubuntu desktop is used — screen time, idle and
locked time and foreground applications, by the hour.

> **Who this is for.** This guide is written for the parent/administrator who
> owns and administers the PC. Parent Tracker is meant for monitoring your own
> minor child on your own device, openly, not for covert surveillance. See
> [Privacy and legal](#18-privacy-and-legal) at the end.

> **Status: first release, not yet run on a real desktop.** The commands,
> screens and settings below exist and are tested, except where a section says
> **Not built yet** (browser history, screenshots, device events, daily digest,
> e-mail alerts, PDF export, time limits). The GNOME Shell extension (app
> tracking on Wayland), the systemd units and the package have been tested only
> against fakes, so expect to run `parent-tracker doctor` after installing and to
> report what it finds. See the [implementation status](11-implementation-status.md).

> **Supported systems.** Ubuntu **24.04 LTS and newer only** (22.04 and older
> are not supported), on **Wayland or X11** sessions. See
> [section 2](#2-requirements-and-capabilities).

## Contents

1. [What it does](#1-what-it-does)
2. [Requirements and capabilities](#2-requirements-and-capabilities)
3. [Install](#3-install)
4. [First-time setup](#4-first-time-setup)
5. [Day-to-day service management](#5-day-to-day-service-management)
6. [The dashboard](#6-the-dashboard)
   - [Live view](#live-view)
   - [Hourly history](#hourly-history)
   - [Exposing the dashboard on your LAN](#exposing-the-dashboard-on-your-lan)
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

- **Session activity** — login/logout and active, idle, locked and suspended
  time, obtained from `systemd-logind`. This works the same on Wayland and X11
  sessions. Screen-off is told apart from idle/locked only where the desktop
  provides a signal; otherwise it counts as idle or locked.
- **Foreground applications** — which app was in front and for how long: read
  directly on X11 sessions, and through a small GNOME Shell extension that the
  package installs on Wayland sessions (see
  [section 2](#2-requirements-and-capabilities)).
- **Browser domains** — *Not built yet.* See [section 10](#10-browser-history).
- **Tracking gaps** — if the service is stopped or the machine crashes, the gap
  is recorded and shown to you rather than silently hidden.

And it shows you that data two extra ways:

- **Live** — what is happening right now (current state, foreground app,
  today's running totals), updating by itself on the dashboard.
- **By hour** — usage broken down per hour of the day, for any day and as a
  7- or 30-day pattern, so you can see *when* the PC was used.

Everything is stored **locally** in a SQLite database. There is no cloud, no
account, and no telemetry.

It does **not**: take screenshots, read messages, record keystrokes, or filter
network traffic (all deferred or out of scope). Browser history, daily digests,
e-mail alerts, PDF export and time limits are planned for later phases.

## 2. Requirements and capabilities

- Ubuntu **24.04 LTS or newer**. This is the minimum: Ubuntu **22.04 LTS and
  older are not supported**, are not tested, and the package will not install on
  them. Official Ubuntu flavours (Kubuntu, Xubuntu, …) based on 24.04 or newer
  are best effort. If your PC is still on 22.04, upgrade Ubuntu to 24.04 first
  (`sudo do-release-upgrade`), then install Parent Tracker.
- **Either a Wayland or an X11 session.** Both are supported (see below); you do
  not have to choose one for the whole PC.
- Application tracking on X11 sessions needs no extra tools: the agent speaks
  the X protocol itself, so nothing from `x11-utils` is required.
- The service runs as root so that the accounts it observes cannot stop it or
  edit its files or database. It is sandboxed with a moderate set of systemd
  directives (no new privileges, read-only system directories). Reading session
  state itself does not need root.
- A small **agent** runs inside each monitored user's graphical session (as that
  user, started automatically). There is nothing to set up.

| Capability | X11 session | Wayland session with the extension | Wayland session without it |
| --- | --- | --- | --- |
| Active / idle / locked time | Yes | Yes | Yes |
| Application (foreground) time | Yes | Yes | Not available |
| Window titles (optional) | Yes | Yes | Not available |
| Browser domains (not built yet) | — | — | — |
| Live state and running totals | Yes | Yes | Yes |
| Live foreground app | Yes | Yes | "App unavailable" |
| Per-hour state time | Yes | Yes | Yes |
| Per-hour app breakdown | Yes | Yes | Not available |

### Wayland and X11 sessions

Ubuntu can display the desktop in two ways, and Parent Tracker supports both:

- **Wayland** — the default on Ubuntu 24.04, and the **only** GNOME session from
  Ubuntu 25.10 on.
- **X11** — still available on Ubuntu 24.04 ("Ubuntu on Xorg", chosen from the
  gear icon on the login screen) and used by X11-only flavours such as Xubuntu.

To see which one a session uses, run `echo $XDG_SESSION_TYPE` in the child's
session (it prints `wayland` or `x11`), or look at `parent-tracker status`,
which lists the session type of every monitored session.

Screen time is read from `systemd-logind` and is **identical** on both. What
differs is how the app in front is found:

- **X11:** the agent reads the active window directly.
- **Wayland:** GNOME deliberately stops programs from reading other programs'
  windows, and a root service cannot get around it. Parent Tracker therefore
  installs a small **GNOME Shell extension** that reports only the app in front
  (and the window title, only if you turned title capture on). The extension has
  no network access and cannot see the screen, keystrokes or messages.

After installing, the package turns the extension on for every account through
a system-wide dconf default. It loads in every account's session but does
nothing unless the tracker agent asks it, and the agent collects only for
monitored accounts. GNOME Shell loads extensions at login, so **ask the child to log out and back in
once.** Check with `parent-tracker doctor`. Until then the dashboard says "App tracking limited" and
screen time is recorded as normal.

The default is also *locked*, so a standard user cannot drop the extension from
their own settings. The side effect is that no standard user can change which
GNOME Shell extensions are enabled on this PC. To undo it:
`sudo /usr/lib/parent-tracker/gnome-extension-setup disable` (and `enable` to
put it back). The list GNOME would use anyway, Ubuntu's own extensions included,
is kept and this one is added to it. **This has been tested against a fake
`gsettings`, not yet on a real Ubuntu desktop.**

If the extension is later disabled or removed, or the agent is stopped, the
dashboard shows "App sensor lost" and you get an alert. Because that data is
collected inside the child's own session, a determined user could interfere with
it; screen time from `systemd-logind` is not affected. This is another reason to
use Parent Tracker openly, with the child's knowledge.

Other desktops (KDE Plasma, Xfce, MATE, …) are supported on a best-effort basis:
screen time is always recorded; app tracking works on X11 sessions with a
standard window manager, and is not available on KDE Plasma under Wayland in the
first release.

Verify your environment at any time with:

```bash
parent-tracker doctor
```

## 3. Install

### 3.1 From the `.deb` package (recommended)

Obtain `parent-tracker_<version>_<arch>.deb` (`<arch>` is `amd64` or `arm64`),
then:

```bash
sudo apt install ./parent-tracker_<version>_<arch>.deb
```

This installs the binaries, the systemd units, the GNOME Shell extension, and a
default configuration. The package:

- creates `/etc/parent-tracker/config.toml` from the shipped example if it does
  not already exist,
- enables (but does not forcibly start) `parent-trackerd.service`,
- installs the per-session agent as a systemd user unit (enabled for all users)
  and an autostart entry that starts it at login on every desktop (Xfce and
  other desktops never start the unit by themselves), and the GNOME Shell
  extension system-wide, switched on and locked through a
  dconf default (see [section 2](#wayland-and-x11-sessions)),
- installs a login notice for monitored users and a **What is recorded about me**
  menu entry,
- creates the data and runtime directories with restrictive permissions.

`apt remove` stops the service and removes the units and the dconf default, and
keeps your configuration and recorded data. `apt purge` deletes both, so run
`parent-tracker export` first if you want a copy.

**Next step:** run the first-time setup (section 4). The service will not
collect anything until you choose which users to monitor.

> **On Ubuntu 22.04 or older?** `apt` stops with an "unmet dependencies" error
> (for example `systemd (>= 255)`) before installing anything. That is
> intentional: only Ubuntu 24.04 and newer are supported. Upgrade the OS first
> (`sudo do-release-upgrade`).

### 3.2 From source

The build needs Go 1.24 or newer on your `PATH` and nothing else (the dashboard
has no build step). Ubuntu 24.04's `golang-go` package is 1.22, which is too old;
install Go from <https://go.dev/dl/> or with `sudo snap install go --classic`:

```bash
git clone https://github.com/samiferr/parent_tracker_ubuntu
cd parent_tracker_ubuntu
make build          # -> bin/parent-trackerd, bin/parent-tracker, bin/parent-tracker-agent
make check          # optional: format, vet and the full test suite
```

To produce a package from source:

```bash
make deb            # -> dist/parent-tracker_<version>_<arch>.deb
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

It walks you through four things.

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

### 4.3 Read and acknowledge the disclosure

Setup shows a plain-language notice saying what is recorded, who can see it, and
that the child can see it too. You must acknowledge it before anything is
recorded. Setup also puts a visible "activity is recorded" entry in the child's
session (and shows a one-time notification if they are logged in), so tracking is
never hidden.

### 4.4 Non-interactive setup

For scripted installs, pass everything as flags. The acknowledgement is explicit
and never implied:

```bash
sudo parent-tracker setup --non-interactive \
  --user ilyes \
  --dashboard-user parent \
  --dashboard-password 'a-strong-password' \
  --acknowledge-disclosure \
  --alert-channel ntfy \
  --ntfy-url 'https://ntfy.sh/my-private-topic'
```

When done, start the service:

```bash
sudo systemctl enable --now parent-trackerd
```

Open <http://127.0.0.1:8787> and sign in. If the child uses a Wayland session,
ask them to log out and back in once (see
[section 2](#wayland-and-x11-sessions)).

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

- **Now (live)** — the current state, the app in front, and today's running
  totals; updates by itself. See [Live view](#live-view).
- **Active time / Idle / Locked** — the day's headline numbers, with a 7-day
  average for context.
- **First / last activity** — when the day's use started and ended.
- **Top applications** — the apps used most in the foreground that day, ranked
  by time.
- **Timeline** — a strip of the day from 00:00 to 24:00: solid blue = active,
  dotted grey = idle, purple stripes = locked, orange hatching = a tracking gap
  (nothing was recorded). A line marks "now". Every pattern is also named in the
  legend, so no meaning rests on colour alone.
- **Trend** — daily active time over 7 or 30 days.
- **Hourly history** — usage by hour of the day. See
  [Hourly history](#hourly-history).
- **Apps** — time in front and time open per application over 7 or 30 days,
  with a drawer per application. (A *Websites* panel is planned with browser
  history; it is not built yet.)
- **Alerts & tracking gaps** — rule hits and any time the service was not
  recording.
- **Audit log** — the service's own record of what it did and what went wrong,
  for the whole PC rather than one account. Choose a period (today, 7 or 30
  days, or your own dates) and a level (errors, warnings, information).
  *Errors* include the app sensor failing, for example "the agent cannot read
  the active window" with the system's own error text, and failures inside the
  service. *Export CSV* and *Export JSON* save the chosen period and level as a
  file (CSV opens in a spreadsheet; the file holds every entry, the screen the
  newest 500). Entries are kept for the `alerts` retention time. The agent's
  own messages are also in `journalctl --user -u parent-tracker-agent`.

Use the **user selector** to switch between monitored accounts and the **date
picker** to look at a specific day.

**Appearance and accessibility.** The dashboard uses the Lumen design system. It
follows your system's light or dark setting (you can override it), works with the
keyboard (a visible focus ring marks where you are), respects the "reduce
motion" setting, and adapts to a phone or tablet screen. It is fully
self-contained: it loads no fonts, scripts or images from the internet, so it
works with no internet connection.

### Live view

The **Now** card at the top of *Today* updates by itself — no refreshing:

- **State** — Active, Idle, Locked, Screen off or Not signed in, and for how
  long it has been in that state.
- **App in front** — the foreground application (the window title only if you
  enabled `collectors.window_title`). On a Wayland session without app tracking
  it says "App unavailable".
- **Today so far** — running Active / Idle / Locked totals, counting up while
  the child is active. The timeline strip below grows to "now".

A change shows up within one sampling interval plus about 2 seconds (about 7
seconds with the default 5 s sampling).

The **indicator next to the card** tells you whether to trust the numbers:

Each indicator is a coloured dot **with a word**, so it never depends on colour
alone:

| Indicator | Meaning |
| --- | --- |
| **Live** (green dot) | Updates are arriving. |
| **Reconnecting…** (orange dot) | The connection dropped; the page is retrying by itself. |
| **Stale — last update 14:32:05** (red dot) | Nothing has arrived for a while (service stopped, network lost). Numbers are greyed out and are *not* current. |
| **Live view off** | Switched off with `server.live = false`; the page updates only when reloaded. |

These colours describe the health of the connection, never the child. The child's
own states (Active, Idle, Locked, …) use neutral colours and are always labelled.

If your browser or a proxy blocks streaming, the dashboard quietly falls back to
asking for a fresh snapshot every few seconds (at least every 5 s); you will see
"polling" in small text instead of an error.

What the live view is **not**: it never shows the screen, keystrokes or message
content. It shows only data that Parent Tracker already records, and the child's
`parent-tracker self` tells them that you can see their current state live
([section 11](#11-the-childs-self-view)).

Settings (apply with `sudo systemctl reload parent-trackerd`):

```bash
sudo parent-tracker config set server.live false            # turn the live view off
sudo parent-tracker config set server.live_max_clients 5    # at most 5 simultaneous live viewers (default 10)
```

### Hourly history

Open **Hourly history** to see *when* the PC was used. Times are in the PC's
local timezone.

**Day view** — 24 bars, one per hour. Each bar is split into **Active**,
**Idle** and **Locked** minutes. Empty space means the child was not signed in
(or the PC was off). A **hatched** bar means a tracking gap: Parent Tracker was
not recording during that time, so it is *unknown*, not "quiet". The hour in
progress is drawn as "in progress" and grows live. Select an hour to see the
**top applications** in that hour.

**7- and 30-day view** — a heatmap: one row per day, one column per hour,
darker = more active minutes. Active hours after your configured `alerts.bedtime`
are outlined, and the hour headings of the bedtime window are underlined, so
late-night use stands out. A row of averages underneath shows the
typical day. Select a cell to jump to that day and hour.

A one-line summary sits above the chart, for example *"Most use 16:00–19:00 ·
last activity 22:40 (after bedtime 21:00) · 3 h 05 m active."*

Good to know:

- **Totals always agree.** The 24 hours of a day add up exactly to that day's
  active time on the other screens.
- **An activity that spans an hour boundary is split.** 10:50–11:20 counts 10
  minutes in the 10:00 hour and 20 in the 11:00 hour.
- **Daylight-saving days** have 23 or 25 hourly bars; the repeated hour is shown
  twice.
- **Per-app detail by hour** needs app tracking (on a Wayland session that means
  the GNOME Shell extension is loaded). If you upgraded from a release that had no
  hourly history, hours recorded before the upgrade are rebuilt from existing
  data: per-hour *totals* from the activity intervals (up to 180 days) but
  per-app detail only as far back as the raw samples go (30 days by default).
  From then on, hourly totals and per-app detail are kept for
  `retention.hourly_usage` (default 1 year).
- **Prefer a table?** Use **Show as table** for the same numbers in a
  screen-reader-friendly list.

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

The live view works the same over the LAN. If you put a reverse proxy in front
of the dashboard, turn off response buffering and compression for
`/api/live/stream`; otherwise the proxy will hold events back and the dashboard
will fall back to polling.

## 7. Command-line reference

All commands accept `-c, --config <path>` (default
`/etc/parent-tracker/config.toml`).

### `parent-tracker version`
Print the version.

### `parent-tracker setup`
Initial configuration. Flags: `--user` (repeatable), `--dashboard-user`,
`--dashboard-password`, `--alert-channel`, `--ntfy-url`,
`--acknowledge-disclosure` (required with `--non-interactive`),
`--non-interactive`, `--reset-config`.

If `/etc/parent-tracker/config.toml` exists but this version cannot read it (for
example a file left by another version, with settings this one does not know),
setup stops and names the settings. `--reset-config` keeps the old file as
`config.toml.bak-<time>` and starts again from the defaults. The package never
replaces an existing configuration, and warns at install time if it cannot be
read.

### `parent-tracker status`
Show config path, data dir, database path, sample interval, monitored users,
schema version, table row counts, collector health, and — for each monitored
session — its **session type** (`wayland` or `x11`) and whether its foreground
provider is working.

### `parent-tracker doctor`
Diagnose everything: **OS support** (Ubuntu 24.04 or newer; on 22.04 or older it
reports "unsupported OS" and exits non-zero), config validity, database
openability and permissions, whether `systemd-logind` is reachable, the session
type of each monitored session, whether its foreground provider works (the X11
provider on X11 sessions; on Wayland sessions whether the GNOME Shell extension
is enabled and reporting, and whether the child still needs to log out and in),
dashboard configuration. On a release that offers no X11 session it reports that
as expected, not as a problem. Run this first when something looks wrong.

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
parent-tracker report --user ilyes --by hour                    # today, hour by hour
parent-tracker report --user ilyes --by hour --date 2026-10-09  # a specific day
parent-tracker report --user ilyes --by hour --from 2026-10-01 --to 2026-10-07 --format csv > hours.csv
```

Flags: `--user`, `--from` (YYYY-MM-DD), `--to`, `--format text|csv`, and
`--by day|hour` (default `day`) with `--date` (YYYY-MM-DD, with `--by hour`).

With `--by hour` there is one line per hour that has data, in local time, showing
active / idle / locked minutes, any tracking-gap minutes and the top app, then
the day's total:

```
ilyes — 2026-10-09 (by hour, local time)
hour    active  idle  locked  gap   top app
16:00   42m     6m    12m     —     firefox (30m)
17:00   51m     4m    5m      —     firefox (35m)
21:00   18m     2m    0m      —     code (12m)
total   1h 51m  12m   17m     0m
```

The CSV output has one row per user-hour with the same columns, and its totals
match the dashboard.

### `parent-tracker export`
Dump recorded data as JSON (users, sessions, intervals, app usage, hourly
usage, alerts, audit), with a manifest containing the version and date range.

```bash
parent-tracker export --out backup.json
parent-tracker export --user ilyes --from 2026-10-01 --to 2026-10-07 --out oct.json
parent-tracker export                 # to stdout
```

### `parent-tracker delete`
Permanently delete a user's data older than a date (real `DELETE`, not hidden).
This includes the per-hour history.

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

`parent-tracker serve --demo [--listen 127.0.0.1:8787]` serves **made-up data**
from a temporary database instead, so you can look around the dashboard before
anything is recorded. The login is `parent` / `demo-password`. Nothing real is
read or written. `make demo` does the same from a source checkout.

### `parent-tracker notice`
Print the notice that activity on this PC is recorded. With `--notify` it shows
the notice as a desktop notification, and only if the account running it is
monitored; the package runs that at every login (an XDG autostart entry), and
adds a **What is recorded about me** menu entry that opens `parent-tracker self`.

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
| `web_import_interval` | `15m` | How often browser history is imported (not built yet; no effect). |
| `control_socket` | `/run/parent-tracker/control.sock` | Socket for the child self-view. |
| `monitored_users` | `[]` | The allowlist. Only these accounts are recorded. |

### `[collectors]` — what to collect

| Key | Default | Meaning |
| --- | --- | --- |
| `session` | `true` | Login/active/idle/locked tracking. |
| `app` | `true` | Foreground application tracking. |
| `window_title` | `false` | Store window titles (privacy-sensitive). |
| `web` | `false` | Import browser history. **Not built yet: no effect.** |
| `web_full_url` | `false` | Store full URLs + page titles instead of domain-only. Requires `web`. **Not built yet: no effect.** |
| `screenshots` | `false` | Reserved for a later phase; no effect in this release. |
| `devices` | `false` | USB/Bluetooth device events (Phase 4); no effect before then. |

### `[retention]` — how long to keep data

| Key | Default | Meaning |
| --- | --- | --- |
| `window_samples` | `720h` (30d) | Raw app samples. |
| `intervals` | `4320h` (180d) | Activity intervals. |
| `app_usage` | `17520h` (730d) | Per-app daily aggregates. |
| `hourly_usage` | `8760h` (365d) | Per-hour history (state time and per-app time by hour). |
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
| `live` | `true` | Real-time dashboard updates. `false` turns the live view off (reload to apply). |
| `live_max_clients` | `10` | Maximum simultaneous live viewers; extra viewers fall back to polling. |

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

Settable keys: `log_level`, `sample_interval`,
`web_import_interval`, `control_socket`, `server.enabled`, `server.listen`,
`server.allow_lan`, `server.live`, `server.live_max_clients`,
`collectors.session`, `collectors.app`,
`collectors.window_title`, `collectors.web`, `collectors.web_full_url`,
`collectors.screenshots`, `alerts.enabled`, `alerts.channel`, `alerts.ntfy_url`,
`alerts.webhook_url`, `alerts.daily_active_limit`, `alerts.bedtime`,
`retention.window_samples`, `retention.intervals`, `retention.app_usage`,
`retention.hourly_usage`, `retention.prune_interval`.

```bash
sudo parent-tracker config set sample_interval 10s
sudo parent-tracker config set collectors.window_title true
sudo parent-tracker config set alerts.daily_active_limit 3h
```

Values that are not settable from the CLI (for example `retention.web_visits`)
can be edited in the file.

## 9. Alerts

Alerts in the first release cover:

- **Daily active-time limit** — fires once per user per day.
- **Usage after bedtime** — fires once per user per day.
- **Tracking interrupted** — a gap or unclean stop.
- **App sensor lost** — the agent was stopped or the GNOME Shell extension was
  disabled or missing, so app tracking stopped (screen time is unaffected).
- **Disk space is low.**

The last three are about whether the tracker can be trusted, so they are
recorded in **Alerts & tracking gaps** even when `alerts.enabled` is `false`;
`alerts.enabled` and `alerts.channel` decide only whether you are *told*. There is
no e-mail channel, and no "app X was used" or "new device" rule yet.

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

Alerts are separate from the live view: alerts *tell* you when something
crosses a rule, while the live view lets you *look* whenever you choose. The
`bedtime` setting is also used by the hourly history to outline late-night hours.

## 10. Browser history

> **Not built yet.** This release does not read any browser's history. The
> `collectors.web` and `collectors.web_full_url` settings are accepted so the
> configuration file will not change later, but turning them on records nothing:
> `config set` says so, `doctor` warns, and the service logs a warning. The rest
> of this section describes the planned behaviour.

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

The dashboard will get a **Websites** panel with the top domains over the last 7
days. Because the service runs as root, it can read browser databases in a monitored
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

  By hour (today, active minutes):
    16:00   42m
    17:00   51m
    21:00   18m

  Applications (last 7 days):
    Firefox Web Browser        1h 05m
    Visual Studio Code         46m

  Websites (last 7 days):
    not recorded (browser history is off)

  Stored about you: activity intervals 5, app usage 3, hourly app usage 9, hourly usage 3, sessions 1, window samples 1332.

Your parent can see your current state and the app in front, live, and your
usage hour by hour. It never sees your screen, keystrokes or messages.
This is everything Parent Tracker records about you. It stays on this computer.
```

The *By hour* section shows the same per-hour history your parent sees for your
account, and the closing note tells you that your parent can see your current
state live. A child should always be able to see everything the parent can.

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

Hour by hour (a single day, or a range as CSV with one row per hour):

```bash
parent-tracker report --user ilyes --by hour --date 2026-10-09
parent-tracker report --user ilyes --by hour --from 2026-10-01 --to 2026-10-31 --format csv > ilyes-october-hours.csv
```

Full JSON export (everything, with a manifest):

```bash
parent-tracker export --out tracker-backup.json
```

## 13. Data retention, deletion and backups

**Retention** keeps the database bounded. Raw samples expire after
`retention.window_samples` (default 30 days) and are removed, but the per-app
daily aggregates are kept much longer (default 2 years), so old trends remain
visible. The per-hour history sits in between: it is kept for
`retention.hourly_usage` (default 1 year), so you can still see *when* the PC was
used long after the raw samples are gone. The pruner runs every
`retention.prune_interval` (default 6h).

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
- the live view is served from memory and does not read the database on every
  update; a dashboard left open costs a negligible amount of CPU and a few KB/s.
  Slow or stuck browser tabs are dropped rather than waited for, and the number
  of simultaneous live viewers is capped (`server.live_max_clients`).

Raw samples are the largest item: a full day of 5-second samples is roughly
1–4 MB per user (less when the PC is used for a few hours), so with the default
30-day retention expect on the order of 60–120 MB per user at most. This is an
estimate that will be confirmed by measurement. Per-hour history adds well under
5 MB per user per year.

## 16. Troubleshooting

**Always start here:**

```bash
parent-tracker doctor
journalctl -u parent-trackerd -n 100 --no-pager
```

| Symptom | Likely cause and fix |
| --- | --- |
| Dashboard says "Sign-in is not set up yet" | Run `sudo parent-tracker setup` to choose a password. |
| Dashboard unreachable from another device | Loopback-only by default; set `server.listen` and `server.allow_lan true`, then restart. |
| No application data on a **Wayland** session | The GNOME Shell extension is not loaded yet (the child has not logged out and in since installation), or it was disabled. Ask them to log out and in, then run `parent-tracker doctor`. |
| No application data on an **X11** session | The agent is not running, cannot reach the X server, or the window manager does not provide the standard active-window information (`_NET_ACTIVE_WINDOW`). Nothing extra needs installing. Check `parent-tracker doctor`, then, **as the monitored user**, `systemctl --user status parent-tracker-agent` and `journalctl --user -u parent-tracker-agent -b` (the agent logs the display it uses and why its sensor is not working). |
| X11 session on **Xubuntu / Xfce**, "The tracker's agent is not running" | Versions before the autostart entry relied on `graphical-session.target`, which Xfce never reaches, so the agent was never started. Upgrade the package and log out and in once. |
| No application data, session type `tty` | A text console has no windows; only session time is recorded. |
| "App sensor lost" | The agent was stopped or the extension disabled in the child's session. See [section 2](#wayland-and-x11-sessions). Screen time is still recorded. |
| `doctor` says the X11 session is not available | Expected on Ubuntu 25.10 and later (Wayland-only GNOME). Not a problem. |
| No web data | Browser history is not built yet (see [section 10](#10-browser-history)). |
| `status` shows a foreground collector `DEGRADED` | Wayland session without the extension, or X11 unreachable; see the rows above. |
| Service won't start | Read `journalctl -u parent-trackerd -n 50`: it names the problem, for example a missing or invalid `/etc/parent-tracker/config.toml` (unknown keys are errors) or systemd-logind not being reachable. |
| "database … too open" in doctor | Fix with `sudo chmod 0640 /var/lib/parent-tracker/tracker.db`. |
| Times look shifted | Day boundaries use the service's local timezone. Set the system timezone correctly. |
| Config edit rejected | Unknown keys are errors. Run `parent-tracker config list` to see valid keys. |
| A monitored user vanished from the list | Their account was removed; `users list` marks stale entries with `!`. |
| `apt` says "unmet dependencies" (e.g. `systemd (>= 255)`) when installing | The PC is on Ubuntu 22.04 or older, which is not supported. Upgrade to 24.04 or newer (`sudo do-release-upgrade`), then install again. |
| `doctor` says "unsupported OS" | Same cause as above. Parent Tracker supports Ubuntu 24.04 and newer only. |
| The Now card says **Stale** or **Reconnecting…** | The dashboard is not receiving updates: check `systemctl status parent-trackerd`, your network, and that `server.live` is not `false`. The page reconnects by itself when the service is back. |
| Live view says "polling", or updates arrive in bursts | Something between you and the service (reverse proxy, VPN, browser extension) is buffering the event stream. Turn off buffering/compression for `/api/live/stream`, or accept the slower polling. |
| "Too many live viewers" | More tabs/devices are open than `server.live_max_clients` allows. Close some, or raise the limit and reload the service. |
| Live view says **App unavailable** | Wayland session without app tracking (expected, see [section 2](#2-requirements-and-capabilities)); state and totals are still live. |
| An hour looks hatched | A tracking gap: the service was not recording then (stopped, crashed, power loss). It is *unknown*, not zero use. See *Alerts & tracking gaps*. |
| An hour is blank with "no data yet" | Before the first recorded activity for that user, or the child was not signed in / the PC was off. |
| No per-app breakdown for an hour | App tracking was unavailable then (Wayland), or the hour is older than the raw-sample retention and was recorded before hourly history existed. Totals are still shown. |
| Hourly bars look an hour off | Hours follow the PC's local timezone and daylight-saving rules. Check the system timezone with `timedatectl`. |

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
| `/usr/lib/systemd/system/parent-trackerd.service` | systemd unit (daemon). |
| `/usr/lib/systemd/user/parent-tracker-agent.service` | systemd user unit for the per-session agent. |
| `/usr/share/gnome-shell/extensions/<uuid>/` | The GNOME Shell extension (Wayland sessions). |
| `/etc/dconf/db/local.d/90-parent-tracker` (and `locks/`) | System-wide default that enables and locks the extension. |
| `/usr/lib/tmpfiles.d/parent-tracker.conf` | Runtime directory definitions. |
| `/usr/lib/parent-tracker/gnome-extension-setup` | Turns the extension's dconf default and lock on or off. |
| `/etc/xdg/autostart/parent-tracker-agent.desktop` | Starts the agent at login on every desktop, through `/usr/lib/parent-tracker/agent-session-start`. |
| `/etc/xdg/autostart/parent-tracker-notice.desktop` | Shows the "activity is recorded" notice at login, for monitored users. |
| `/usr/share/applications/parent-tracker-self.desktop` | The "What is recorded about me" menu entry. |
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
- The live view and the hourly history show **only what is already recorded**
  (state, foreground app, totals by hour). They never show the screen, keystrokes
  or message content, and the child can see the same data — including that you
  can watch live — with `parent-tracker self`. Hour-by-hour patterns are more
  revealing than daily totals, which is why they are kept for 1 year rather than
  2; shorten `retention.hourly_usage` if you want less.
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

**Does it work on Ubuntu 22.04?**
No. Parent Tracker supports Ubuntu 24.04 LTS and newer only. On 22.04 the
package will not install (`apt` reports unmet dependencies). Upgrade Ubuntu to
24.04 first; your `/etc/parent-tracker` and `/var/lib/parent-tracker` are not
touched by the OS upgrade.

**Does it work on the newest Ubuntu releases?**
Yes, 24.04 and later are supported. Ubuntu 25.10 and later offer only a Wayland
GNOME session; app tracking there uses the GNOME Shell extension, so the child
needs to log out and in once after installation. See
[section 2](#wayland-and-x11-sessions).

**Do I need Wayland or X11?**
Either. Both are supported, and one PC can be used in either from one login to
the next, or by different children at the same time. Screen time is identical;
only the way the app in front is found differs.

**Can my child avoid tracking by switching between Wayland and X11?**
No. Session time is recorded either way, and each session's app tracking is
checked separately. Switching just moves between the two providers.

**Can my child turn off the GNOME extension?**
The extension is enabled system-wide and set up so a standard user cannot simply
remove it. Someone determined can still interfere with app data in their own
session, so Parent Tracker records it ("App sensor lost"), shows it on the
dashboard and alerts you. Screen time from `systemd-logind` is unaffected.

**Why does the child have to log out and in after installation?**
GNOME Shell loads extensions only when a session starts, and on Wayland it
cannot be restarted inside a session.

**How accurate is "active" time?**
Idle comes from the desktop session's idle setting (on Ubuntu the screen blank /
idle delay is 5 minutes by default). Parent Tracker back-dates idle to when it
actually began, so a few minutes of inactivity before it is detected are not
counted as use. Accuracy targets are in [docs/08](08-testing-acceptance.md).

**Does the dashboard update by itself?**
Yes. The *Now* card and today's totals refresh automatically; the Live /
Reconnecting / Stale indicator tells you if they are current. You never need to
reload the page.

**Can I watch my child's screen live?**
No, and by design. The live view shows the state (active/idle/locked), the app in
front and running totals, nothing else.

**Will the child know I can see live activity?**
Yes. `parent-tracker self` tells them, and shows them the same hourly history
you see.

**Can I turn the live view off?**
Yes: `sudo parent-tracker config set server.live false`, then reload the
service. The dashboard keeps working and updates when you reload the page.

**How far back does the hourly history go?**
Per-hour totals are kept for `retention.hourly_usage` (default 1 year). Per-app
detail for each hour is available for hours recorded after hourly history was
introduced and, for older data, as far back as the raw samples go (30 days by
default).

**Can I see full URLs?**
Only if you enable `collectors.web_full_url`. Otherwise only domains are stored.

**How do I move the data to a new PC?**
`parent-tracker export` on the old machine, then reinstall and set up on the
new one; the export is JSON for archival/reference (there is no import command
yet).
