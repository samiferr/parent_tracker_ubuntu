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
| FR-2.3 | MUST | Distinguish session states: **active**, **idle**, **locked/screensaver**, **screen-off**, **suspended**, **logged-out**. |
| FR-2.4 | MUST | Compute **active time** as time in `active` state only; never count locked/idle time as usage. |
| FR-2.5 | MUST | Detect idle via system sources (logind idle hint / D-Bus screensaver) rather than only synthetic input polling. |
| FR-2.6 | SHOULD | Record boot/shutdown and suspend/resume boundaries so gaps are explainable. |
| FR-2.7 | SHOULD | Support multiple monitored users active at once (fast user switching) and attribute time correctly. |
| FR-2.8 | MAY | Record remote sessions (SSH) separately and exclude them from "screen time" by default. |

## 2.3 Application & window tracking

| ID | Priority | Requirement |
| --- | --- | --- |
| FR-3.1 | MUST | On X11, detect the **foreground window** (app, title, PID) on a configurable interval (default 5 s). |
| FR-3.2 | MUST | On Wayland (GNOME), obtain the equivalent foreground-app signal via supported mechanisms (GNOME Shell extension / D-Bus / `org.gnome.Shell` APIs) or degraded fallback. |
| FR-3.3 | MUST | Map windows/PIDs to human-readable **application names** (desktop entry / `.desktop` file name / executable). |
| FR-3.4 | MUST | Aggregate per-app time per day based on foreground samples, merging windows of the same app. |
| FR-3.5 | SHOULD | Capture window **title** (optional, can be disabled for privacy) to distinguish e.g. document tabs. |
| FR-3.6 | SHOULD | Track **idle-with-app-open** separately so "Chrome open all day" ≠ "Chrome used all day". |
| FR-3.7 | SHOULD | Support per-app **categorisation** (games, school, social, video) via a configurable rules file. |
| FR-3.8 | MAY | Track foreground app in full-screen/game sessions where no title updates occur. |

## 2.4 Web activity (phase 2)

| ID | Priority | Requirement |
| --- | --- | --- |
| FR-4.1 | SHOULD | Record visited domains/URLs for supported browsers (Firefox, Chrome/Chromium) via browser history DB or extension, **not** via decryption of TLS. |
| FR-4.2 | SHOULD | Be able to store **domain only** (default) or full URL (opt-in). |
| FR-4.3 | MUST | Web tracking is individually switchable and **off by default**; enabling it is a deliberate admin action. |
| FR-4.4 | MAY | Search-query capture (opt-in, high sensitivity; clearly surfaced to the child). |

## 2.5 Screenshots (phase 3, optional)

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
| FR-8.5 | SHOULD | `parent-tracker doctor` diagnoses Wayland/X11 support, permissions, and collector health. |
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
| FR-10.1 | MUST | Raw events are retained for a configurable window (default 90 days); older data is aggregated then deleted. |
| FR-10.2 | MUST | Admin can export all data (JSON/CSV) and **delete** data for a user or date range. |
| FR-10.3 | SHOULD | The child can see (read-only) their own recorded data. |
| FR-10.4 | MUST | Deletion is real (records removed), not just hidden. |
