# 02 – Functional Requirements

Requirement IDs are stable and referenced by tests and the roadmap.
**Priority:** `MUST` (MVP), `SHOULD` (target), `MAY` (nice to have).

---

## 2.1 Service & lifecycle

| ID | Priority | Requirement |
| --- | --- | --- |
| FR-1.1 | MUST | The product installs and runs as a **system-wide systemd service** (`parent-trackerd`) that starts at boot, independent of any user login. |
| FR-1.2 | MUST | The service survives user logout, fast user switching, suspend/resume, and desktop-shell crashes without requiring manual restart. |
| FR-1.3 | MUST | The service can be started, stopped, restarted, enabled, and disabled via standard `systemctl` commands. |
| FR-1.4 | MUST | Configuration lives in `/etc/parent-tracker/config.toml` (or equivalent); secrets/permissions are not world-readable. |
| FR-1.5 | MUST | Persistent data lives under `/var/lib/parent-tracker/`; logs under `/var/log/parent-tracker/` (or the journal). |
| FR-1.6 | SHOULD | The service supports a **dry-run / diagnostic mode** that reports what it can and cannot observe without writing data. |
| FR-1.7 | SHOULD | The service self-reports health (last successful sample, collector status) exposed to the dashboard and to `systemctl status`. |
| FR-1.8 | MAY | Automatic restart with backoff on repeated failure, with a "flapping" log warning. |

## 2.2 User & session tracking

| ID | Priority | Requirement |
| --- | --- | --- |
| FR-2.1 | MUST | The admin defines a **monitored-users allowlist** (by username/UID). Only these accounts are tracked. |
| FR-2.2 | MUST | Record session start (login/unlock) and end (logout/lock/shutdown) with timestamps, per monitored user. |
| FR-2.3 | MUST | Distinguish session states: **active**, **idle**, **locked/screensaver**, **suspended**, **logged-out**. Distinguishing **screen-off** is SHOULD: systemd-logind has no screen-off signal, so it needs a compositor-specific source (see [04 §4.3](04-architecture.md#43-collection-mechanisms-per-environment)); where unavailable it is folded into idle/locked. |
| FR-2.4 | MUST | Compute **active time** as time in `active` state only; never count locked/idle time as usage. |
| FR-2.5 | MUST | Detect idle via system sources (logind idle hint / D-Bus screensaver) rather than only synthetic input polling. |
| FR-2.6 | SHOULD | Record boot/shutdown and suspend/resume boundaries so gaps are explainable. |
| FR-2.7 | SHOULD | Support multiple monitored users active at once (fast user switching) and attribute time correctly. |
| FR-2.8 | MAY | Record remote sessions (SSH) separately and exclude them from "screen time" by default. |
| FR-2.9 | MUST | Detect the **session type** (`wayland`, `x11`, `tty`) of every session from systemd-logind and store it with the session; choose collectors per session accordingly. Login-screen (greeter) sessions are never tracked. |

## 2.3 Application & window tracking

Both **Wayland and X11** sessions must be supported (NFR-3.3); the detailed
support matrix and rationale are in [04 §4.13](04-architecture.md#413-session-types-wayland-and-x11).

| ID | Priority | Requirement |
| --- | --- | --- |
| FR-3.1 | MUST | On an **X11** session, detect the **foreground window** (app, title, PID) on a configurable interval (default 5 s) using EWMH (`_NET_ACTIVE_WINDOW`). |
| FR-3.2 | MUST | On a **Wayland** GNOME session, obtain the foreground app from the project's **GNOME Shell extension**, which is the only supported route: the compositor hides other clients' windows, and GNOME's `org.gnome.Shell.Introspect` interface is restricted to allow-listed callers (the desktop portals), so a root daemon cannot use it. |
| FR-3.3 | MUST | Map windows/PIDs to human-readable **application names** (desktop entry / `.desktop` file name / executable). |
| FR-3.4 | MUST | Aggregate per-app time per day based on foreground samples, merging windows of the same app. |
| FR-3.5 | SHOULD | Capture window **title** (optional, can be disabled for privacy) to distinguish e.g. document tabs. |
| FR-3.6 | SHOULD | Track **idle-with-app-open** separately so "Chrome open all day" ≠ "Chrome used all day". |
| FR-3.7 | SHOULD | Support per-app **categorisation** (games, school, social, video) via a configurable rules file. |
| FR-3.8 | MAY | Track foreground app in full-screen/game sessions where no title updates occur. |
| FR-3.9 | MUST | The X11 provider is used **only** for `x11` sessions. On a `wayland` session it must not be used (XWayland exposes only X11 clients and would attribute time to the wrong or no window); without the extension, app data is reported as unavailable, never guessed. |
| FR-3.10 | MUST | The core metrics (active / idle / locked time, sessions, gaps) come from systemd-logind and are **identical on Wayland and X11**; they must not depend on a foreground provider or on the session type. |
| FR-3.11 | MUST | When app tracking is unavailable or degraded (Wayland without the extension, X11 unreachable, tty), say so in `doctor`, `/api/health` and a dashboard banner, and keep recording screen time. |
| FR-3.12 | SHOULD | The package installs the GNOME Shell extension system-wide and enables it by default for monitored users, so that after one log-out/log-in no manual step is needed; loss of the extension (disabled, crashed, missing heartbeat) is logged and surfaced (SEC-35). |
| FR-3.13 | MAY | Foreground-app support for KDE Plasma on Wayland (KWin scripting) and other compositors; until then those sessions record screen time only. |

## 2.4 Web activity (Phase 4, opt-in)

| ID | Priority | Requirement |
| --- | --- | --- |
| FR-4.1 | SHOULD | Record visited domains/URLs for supported browsers (Firefox, Chrome/Chromium) via browser history DB or extension, **not** via decryption of TLS. |
| FR-4.2 | SHOULD | Be able to store **domain only** (default) or full URL (opt-in). |
| FR-4.3 | MUST | Web tracking is individually switchable and **off by default**; enabling it is a deliberate admin action. |
| FR-4.4 | MAY | Search-query capture (opt-in, high sensitivity; clearly surfaced to the child). |

## 2.5 Screenshots (Phase 7, optional)

| ID | Priority | Requirement |
| --- | --- | --- |
| FR-5.1 | MAY | Periodic screenshots at a configurable interval, stored encrypted, with short retention. |
| FR-5.2 | MAY | Screenshots are **off by default**, rate-limited, and clearly disclosed. |
| FR-5.3 | MAY | A visible indicator/notification when a screenshot is taken (anti-spyware stance). |

## 2.6 Reports & dashboard

| ID | Priority | Requirement |
| --- | --- | --- |
| FR-6.1 | MUST | Provide a **local web dashboard** (default `http://127.0.0.1:<port>`) showing today's usage: active time, top apps, timeline. |
| FR-6.2 | MUST | Show **daily** and **weekly** summaries with trends; allow selecting a date range per monitored user. |
| FR-6.3 | MUST | Display a timeline/gantt of activity (active/idle/locked) over the day. |
| FR-6.4 | MUST | Dashboard access is authenticated; not exposed to the network by default. |
| FR-6.5 | SHOULD | Export reports as CSV and PDF for records. |
| FR-6.6 | SHOULD | Configurable **daily digest** summary (e.g., generated at 21:00) delivered by the alert channel. |
| FR-6.7 | MAY | Per-app and per-category limits shown as "budget used / remaining". |

### 2.6.1 Real-time dashboard

| ID | Priority | Requirement |
| --- | --- | --- |
| FR-6.8 | MUST | The dashboard has a **live view** for the selected monitored user, updated automatically with no page reload: current session state (**active / idle / locked / screen-off / logged-out**), current foreground app, how long the current state has lasted, and today's running active/idle/locked totals. |
| FR-6.9 | MUST | A state or app change is reflected on an open dashboard within **one sampling interval + 2 s** (default ≤ 7 s). |
| FR-6.10 | MUST | Live updates are pushed by the daemon over a single authenticated, same-origin connection (**Server-Sent Events**); if streaming is unavailable the dashboard transparently falls back to polling a snapshot endpoint at the sampling interval (minimum 5 s). |
| FR-6.11 | MUST | The live view always shows its own freshness: a **Live** indicator while updates arrive, a **Reconnecting / Stale** indicator with "last updated hh:mm:ss" when they stop, and automatic reconnection with back-off. A stale view never presents old numbers as current. |
| FR-6.12 | MUST | Live totals include the **in-progress interval** (clamped to the last good sample so a stalled collector does not inflate them) and must agree with the stored daily figures after the next rollup (±1 sampling interval). |
| FR-6.13 | MUST | The timeline strip on the Today screen extends to "now" as the day progresses, and tracking gaps appear live. |
| FR-6.14 | MUST | The live view exposes **only data that is already recorded**: window titles appear only if `collectors.window_title` is on; on Wayland without app tracking it shows "app unavailable" rather than guessing. No screen content, ever. |
| FR-6.15 | SHOULD | The live feature can be switched off (`server.live`) and its concurrent-connection count is bounded (`server.live_max_clients`). |
| FR-6.16 | MAY | A browser tab-title / favicon badge showing the current state, so a pinned tab acts as a glanceable indicator. |

### 2.6.2 Per-hour history

| ID | Priority | Requirement |
| --- | --- | --- |
| FR-6.20 | MUST | Record and report usage **per local hour**: for each user and hour, time in `active`, `idle`, `locked/screen-off`, and untracked (**gap**) time. |
| FR-6.21 | MUST | Activity intervals that cross an hour boundary are **split exactly at the boundary** (10:50–11:20 counts 10 min in the 10:00 hour and 20 min in the 11:00 hour). |
| FR-6.22 | MUST | Summing the 24 hourly buckets of a day equals that day's daily active time, exactly. Daily and hourly figures come from the same source and can never disagree. |
| FR-6.23 | MUST | Per-hour **application** breakdown: the top apps (foreground time) in each hour, where app tracking works; the same map-and-merge rules as FR-3.3/3.4. |
| FR-6.24 | MUST | Hourly history is kept **longer than raw samples** (default 1 year, configurable via `retention.hourly_usage`), so "when" patterns survive raw-sample expiry. |
| FR-6.25 | MUST | Hour boundaries follow the machine's **local timezone**, including DST transitions (23- and 25-hour days) and zones with non-whole-hour offsets. Gaps and the in-progress hour are shown honestly, never as zero usage. |
| FR-6.26 | MUST | Dashboard **Hourly history** screen: (a) a single-day 24-hour chart with per-hour drill-down to apps, and (b) a multi-day hour-of-day heatmap (7 / 30 days) showing when usage typically happens. |
| FR-6.27 | SHOULD | `parent-tracker report --by hour` and the hourly API/CSV output give the same numbers as the dashboard. |
| FR-6.28 | SHOULD | Hours after the configured `alerts.bedtime` are visually marked in the hourly views. |
| FR-6.29 | MAY | Typical-week view: average active minutes per hour for each weekday. |

## 2.7 Alerts & notifications

| ID | Priority | Requirement |
| --- | --- | --- |
| FR-7.1 | SHOULD | Send alerts on configurable conditions (e.g., daily active time > threshold, app X used, usage after bedtime, new device connected). |
| FR-7.2 | SHOULD | Support at least one channel: local desktop notification, email (SMTP), or **ntfy**/webhook. |
| FR-7.3 | MUST | Alerts are rate-limited and de-duplicated to avoid spam. |
| FR-7.4 | MAY | Weekly digest email with attached report. |

## 2.8 Configuration & administration

| ID | Priority | Requirement |
| --- | --- | --- |
| FR-8.1 | MUST | A **CLI** (`parent-tracker`) to add/remove monitored users, set retention, enable collectors, and export data. |
| FR-8.2 | MUST | Configuration changes take effect without reinstall; service reload via `systemctl reload` or SIGHUP. |
| FR-8.3 | SHOULD | The dashboard exposes the same settings with validation. |
| FR-8.4 | MUST | Sensible, documented defaults; the system never silently starts tracking a new user. |
| FR-8.5 | SHOULD | `parent-tracker doctor` diagnoses OS support (Ubuntu 24.04+), the session type of each monitored user's session, whether the right foreground provider is working (X11 reachable / GNOME extension enabled and reporting), permissions, and collector health. |
| FR-8.6 | SHOULD | Uninstall removes the service and (with confirmation) its data. |

## 2.9 Enforcement (later phase, opt-in)

| ID | Priority | Requirement |
| --- | --- | --- |
| FR-9.1 | MAY | Enforce **daily time budgets** (pause/notify/logout when exceeded). |
| FR-9.2 | MAY | Enforce **bedtimes/schedules** for the monitored account. |
| FR-9.3 | MAY | Block or warn on apps/domains by rule. |
| FR-9.4 | MUST | Any enforcement shows a clear, non-deceptive message to the child; no silent blocking. |
| FR-9.5 | MUST | Enforcement is independent of collection and can be disabled without affecting monitoring. |

## 2.10 Data export, retention & deletion

| ID | Priority | Requirement |
| --- | --- | --- |
| FR-10.1 | MUST | Raw samples are retained for a configurable window (default **30 days**, see [05 §5.2](05-data-model.md#52-retention-policy)); older data survives only as aggregates. |
| FR-10.2 | MUST | Admin can export all data (JSON/CSV) and **delete** data for a user or date range. |
| FR-10.3 | SHOULD | The child can see (read-only) their own recorded data. |
| FR-10.4 | MUST | Deletion is real (records removed), not just hidden. |
| FR-10.5 | MUST | Hourly history (FR-6.20…6.25) is covered by export, by `delete`, and by the child's self-view exactly like every other recorded dataset — nothing is stored that the child cannot see or the admin cannot delete. |
