# 01 – Overview & Scope

## 1.1 Problem statement

A parent wants an accurate, low-effort picture of how their child uses the
family's Ubuntu desktop PC: how much time is spent, on what, and when. Manual
spot-checks are unreliable, and generic "screen time" apps are built for
Windows/macOS. There is no simple, self-hosted, Linux-native option that is
transparent to the child and controllable by the parent.

## 1.2 Goals (must achieve)

- **G1** — Accurately measure *active* computer usage time per monitored user,
  separated from idle/locked time.
- **G2** — Attribute usage to applications (and windows) for a useful breakdown.
- **G3** — Present this in a form a busy parent can read in under a minute
  (daily summary, weekly trend).
- **G4** — Run unattended as a system service, surviving reboots and user
  logouts, with negligible performance impact.
- **G5** — Keep all data local to the family by default, with an explicit
  opt-in path to sync elsewhere.
- **G6** — Be honest and transparent: the monitored child can see what is being
  collected.
- **G7** — Show what is happening **right now** (a live dashboard) without the
  parent having to reload anything.
- **G8** — Show *when* the PC was used: usage broken down **per hour**, for a
  single day and as a history over weeks, not only daily totals.

## 1.3 Non-goals (out of scope for now, may revisit)

- Covert/silent surveillance.
- Keystroke logging or reading message contents.
- Real-time **screen** streaming / remote control. (The live dashboard in G7
  shows status only — current state, foreground app, running totals — never
  screen content, keystrokes or window contents.)
- Blocking or filtering network traffic (this is monitoring, not a firewall).
- Mobile/Android or Windows/macOS support.
- Multi-tenant / commercial SaaS operation.

## 1.4 Personas

| Persona | Description | Primary needs |
| --- | --- | --- |
| **Parent / Administrator** | Owns the PC, installs and configures the service, holds the admin password. | Clear reports, alerts, easy configuration, confidence data is safe. |
| **Monitored child** | Standard user account being tracked. | Knows what is collected; fair, non-creepy limits. |
| **Admin sibling / other users** | Other accounts on the same PC. | Not to be tracked unless explicitly listed. |
| **Maintainer** | The person building this (you). | Clear requirements, sensible architecture. |

## 1.5 Scope boundaries

### In scope

- One physical Ubuntu desktop PC, multiple local user accounts.
- **Ubuntu 24.04 LTS and newer only** (see
  [03 – Non-Functional Requirements](03-non-functional-requirements.md#33-compatibility)).
- A **monitored-users allowlist**; accounts not on it are never tracked.
- Session, activity and application collection on **Wayland and X11** desktop
  sessions (MVP, Phases 1–2); web history and device events (Phase 4, opt-in);
  screenshots (Phase 7, opt-in, optional).
- A browser dashboard built with the **Lumen design system**
  ([12 – Design System: Lumen](12-design-system-lumen.md)).
- Local storage, local reporting, optional alerting.
- Packaging, install, upgrade, uninstall.
- Optional parental enforcement (time limits, blocks) — later phase.

### Out of scope (this version)

- Central management of many PCs (single-machine only in MVP).
- Cloud accounts.
- Deep content inspection (OCR, chat content).
- **Ubuntu 22.04 LTS and older releases** — not supported and not tested; the
  package refuses to install there. Upgrade the OS to 24.04 or newer first.

## 1.6 Terminology

| Term | Meaning |
| --- | --- |
| **Service** | The `parent-trackerd` systemd system service (daemon). |
| **Collector** | A sub-component that gathers one kind of signal (session, window, …). |
| **Agent / per-user helper** | A small per-session process (systemd user unit) that runs as the monitored user and collects GUI-level data (foreground app) the system daemon cannot or should not collect itself. |
| **Session type** | How a graphical login session is displayed: `wayland`, `x11`, or `tty` (no GUI). Reported by systemd-logind. |
| **Foreground provider** | The component that learns which app has focus in one session: the X11 provider (EWMH) or the GNOME Wayland provider (Shell extension). |
| **GNOME Shell extension** | A small extension shipped with the package that reports the focused window inside a GNOME **Wayland** session. |
| **Lumen** | The design system the dashboard UI must be built with ([12](12-design-system-lumen.md)). |
| **Monitored user** | A local account explicitly listed for tracking. |
| **Event** | A timestamped, immutable record of something that happened (login, app focus, idle start). |
| **Sample** | A periodic measurement (e.g., active window every 5 s) used to derive durations. |
| **Active time** | Time when a session is unlocked, non-idle, and the screen is on. |
| **Dashboard** | Local web UI for viewing data and configuring the service. |
| **Live view** | The part of the dashboard that updates by itself (current state, foreground app, running totals) without a page reload. |
| **Hourly bucket** | One local wall-clock hour (e.g. 15:00–15:59) of a given day, holding the time spent in each state and in each app during that hour. |
| **Retention** | How long raw events are kept before aggregation/expiry. |

## 1.7 Success metrics

- **S1** — A parent can answer "how much did my kid use the PC today and on
  what?" from the dashboard in < 60 seconds.
- **S2** — Reported active time is within ±5% of ground truth in validation.
- **S3** — Steady-state CPU usage < 1% of one core, RAM < 100 MB.
- **S4** — Zero data loss across unclean shutdown (events persist or are
  reconstructable).
- **S5** — Install → working dashboard in under 10 minutes.
- **S6** — A change of state (active → locked, app switch) appears on an open
  dashboard within one sampling interval plus 2 s, with no manual reload.
- **S7** — A parent can answer "what time of day did my kid use the PC, and
  what were they doing at 21:00?" from the dashboard in < 60 seconds.

## 1.8 Regulatory / ethical stance

This is **parental monitoring of a minor on a device the parent owns and
administers**. The project nonetheless treats the child as a person with a
reasonable expectation of dignity: no covert mode, no content interception,
and the child can see what is stored about them. See
[06 – Security, Privacy & Legal](06-security-privacy-and-legal.md).
