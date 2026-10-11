# 09 – Roadmap

Phased delivery. Each phase is independently useful and shippable.

> **Status (2026-10-11): no phase has been started; no code exists.** See
> [11 – Implementation Status](11-implementation-status.md).

## Phase 0 — Foundations (1–2 weeks)
- Repo layout, language choice (open question Q1), build/CI, `.deb` skeleton for
  **amd64 and arm64**.
- systemd service skeleton that starts at boot and logs.
- Config file + CLI skeleton (`status`, `doctor` stub).
- SQLite schema + migrations.
- Dashboard shell: React 18 + **Lumen** vendored with a `LUMEN_VERSION` pin,
  self-hosted fonts, light/dark, served from the embedded assets with **no
  external requests**; front-end build tool chosen; third-party notices file
  (resolves Q15).
- **Exit:** service installs on Ubuntu 24.04 (the minimum supported release) and
  reports "alive"; the empty dashboard shell passes AC-110…AC-113, AC-116.

## Phase 1 — MVP: session & screen time (2–3 weeks)
- logind/D-Bus session + **session-type detection** (`Type`, `Class`, `Remote`) +
  lock/idle collection. Validation spike on a real 24.04 machine in **both** a
  Wayland and an Xorg session (04 §4.13.7).
- Active/idle/locked/suspended intervals; daily active time.
- Monitored-user allowlist.
- First-run **disclosure acknowledgement** and the child's minimal self-view
  (`parent-tracker self`) — they are central to the project's ethics (SEC-1,
  SEC-3, Q11), so they ship with the first usable version.
- Minimal dashboard in Lumen: today's active time + timeline + top apps placeholder.
- Retention/pruner.
- **Exit:** AC-1…AC-6, AC-71, AC-102, AC-103 pass; AC-30 partially.

## Phase 2 — Application tracking (3–4 weeks)
- Foreground-provider abstraction and the per-session **agent** (systemd user unit).
- **Wayland first** (the default session on 24.04 and the only GNOME session
  from 25.10): GNOME Shell extension, system-wide enablement, heartbeat and
  tamper evidence (SEC-35).
- X11 provider (EWMH) for 24.04 Xorg sessions and X11 flavours; never used on
  Wayland sessions (FR-3.9).
- App mapping, per-app aggregates, categories.
- Dashboard: top apps, trends, app detail.
- **Exit:** AC-10…AC-13, AC-31, AC-100…AC-105, AC-107, AC-108 pass.

## Phase 3 — Reporting, alerts, multi-user polish (2–3 weeks)
- Daily/weekly digest, CSV/PDF export.
- Alert engine + one channel (ntfy or email).
- Multi-user/fast-switch correctness; audit log + gap surfacing.
- **Exit:** AC-20, AC-21, AC-32, AC-42, AC-53, AC-106 pass.

## Phase 4 — Web activity & devices (optional, opt-in) (2–3 weeks)
- Firefox/Chromium history import (domain-only default).
- udev device events.
- Dashboard web/device views. High-sensitivity defaults off.
- **Exit:** AC-70/71 and web-device acceptance tests pass.

## Phase 5 — Hardening & packaging polish
- systemd hardening, encryption-at-rest option, TOTP dashboard auth.
- Upgrade/migration and purge flows; soak test.
- Verify arm64 packages and the full matrix on both architectures.
- **Exit:** AC-40/41/50/51/52/54/60/61/62 pass.

## Phase 6 — Enforcement (optional, later)
- Time budgets, bedtimes, app/domain rules with visible messaging.
- **Exit:** enforcement tests pass; collection still works when disabled.

## Phase 7 — Nice-to-haves
- Optional multi-machine sync (explicit, encrypted).
- Screenshots (opt-in, disclosed, short retention).
- Search-term capture (opt-in, high sensitivity).
- Localisation.

## Phase 8 — Platform baseline, hourly history & live dashboard (next)
Phase numbers are stable IDs used elsewhere in these docs; Phase 8 is scheduled
**ahead of Phases 6 and 7**. Suggested order inside the phase:

1. **Platform baseline: Ubuntu 24.04+ only** (NFR-3.1, 3.7, 3.8, 6.8).
   - `.deb` `Depends: systemd (>= 255)` (and `libc6 (>= 2.39)` if dynamic).
   - `doctor` OS-support check.
   - Drop 22.04 from CI and the manual matrix; add the negative install test.
2. **Per-hour history** (FR-6.20…6.29, FR-10.5).
   - `hourly_usage` / `hourly_app_usage`, migration + background backfill.
   - Hourly rollup with 48 h idempotent recompute.
   - `/api/hourly`, Hourly history screen, `report --by hour`, export / delete /
     self-view coverage, `retention.hourly_usage`.
3. **Real-time dashboard** (FR-6.8…6.16).
   - In-memory live hub, `/api/live` + `/api/live/stream` (SSE), polling
     fallback, freshness indicator, Live "Now" card, live timeline edge.
   - `server.live`, `server.live_max_clients`; disclosure/self-view wording
     (SEC-6).
   - Hourly goes first because the Live card's "current hour" reuses it.
- The hourly chart, heatmap and live indicator are built from Lumen tokens
  (12 §12.6, §12.7) and must pass AC-110…AC-117.
- **Exit:** AC-60…AC-64, AC-80…AC-88, AC-90…AC-98 pass.

## Prioritisation rationale

Value order: **screen time → which apps → reports/alerts → web → enforcement**.
Each phase answers a real parent question. Wayland app tracking is the biggest
technical risk and is isolated to Phase 2 so earlier value isn't blocked by it —
and because screen time comes from logind, Phases 1, 3 and 8 deliver value on
both session types even before Wayland app tracking works. Supporting only
Ubuntu 24.04+ makes Wayland the *main* path rather than an edge (newer releases
are Wayland-only on GNOME), which is why Phase 2 builds the Wayland extension
before the X11 provider, and why Phase 8's live and hourly views must degrade
cleanly where per-app data is unavailable.
