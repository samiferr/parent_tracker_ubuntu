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

## 1.3 Non-goals (out of scope for now, may revisit)

- Covert/silent surveillance.
- Keystroke logging or reading message contents.
- Real-time screen streaming / remote control.
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
- A **monitored-users allowlist**; accounts not on it are never tracked.
- Session, activity, application, and (phase 2) web/screenshot collection.
- Local storage, local reporting, optional alerting.
- Packaging, install, upgrade, uninstall.
- Optional parental enforcement (time limits, blocks) — later phase.

### Out of scope (this version)

- Central management of many PCs (single-machine only in MVP).
- Cloud accounts.
- Deep content inspection (OCR, chat content).

## 1.6 Terminology

| Term | Meaning |
| --- | --- |
| **Service** | The `parent-trackerd` systemd system service (daemon). |
| **Collector** | A sub-component that gathers one kind of signal (session, window, …). |
| **Agent / per-user helper** | An optional per-session process that can see GUI data the system daemon cannot. |
| **Monitored user** | A local account explicitly listed for tracking. |
| **Event** | A timestamped, immutable record of something that happened (login, app focus, idle start). |
| **Sample** | A periodic measurement (e.g., active window every 5 s) used to derive durations. |
| **Active time** | Time when a session is unlocked, non-idle, and the screen is on. |
| **Dashboard** | Local web UI for viewing data and configuring the service. |
| **Retention** | How long raw events are kept before aggregation/expiry. |

## 1.7 Success metrics

- **S1** — A parent can answer "how much did my kid use the PC today and on
  what?" from the dashboard in < 60 seconds.
- **S2** — Reported active time is within ±5% of ground truth in validation.
- **S3** — Steady-state CPU usage < 1% of one core, RAM < 100 MB.
- **S4** — Zero data loss across unclean shutdown (events persist or are
  reconstructable).
- **S5** — Install → working dashboard in under 10 minutes.

## 1.8 Regulatory / ethical stance

This is **parental monitoring of a minor on a device the parent owns and
administers**. The project nonetheless treats the child as a person with a
reasonable expectation of dignity: no covert mode, no content interception,
and the child can see what is stored about them. See
[06 – Security, Privacy & Legal](06-security-privacy-and-legal.md).
