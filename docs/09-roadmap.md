# 09 – Roadmap

Phased delivery. Each phase is independently useful and shippable.

## Phase 0 — Foundations (1–2 weeks)
- Repo layout, language choice, build/CI, `.deb` skeleton.
- systemd service skeleton that starts at boot and logs.
- Config file + CLI skeleton (`status`, `doctor` stub).
- SQLite schema + migrations.
- **Exit:** service installs on 22.04/24.04 and reports "alive".

## Phase 1 — MVP: session & screen time (2–3 weeks)
- logind/D-Bus session + lock/idle collection.
- Active/idle/locked intervals; daily active time.
- Monitored-user allowlist.
- Minimal dashboard: today's active time + timeline + top apps placeholder.
- Retention/pruner.
- **Exit:** AC-1…AC-6 pass; AC-30 partially.

## Phase 2 — Application tracking (3–4 weeks)
- X11 foreground window collector.
- Wayland agent + GNOME Shell extension (or supported API).
- App mapping, per-app aggregates, categories.
- Dashboard: top apps, trends, app detail.
- **Exit:** AC-10…AC-13, AC-31 pass.

## Phase 3 — Reporting, alerts, multi-user polish (2–3 weeks)
- Daily/weekly digest, CSV/PDF export.
- Alert engine + one channel (ntfy or email).
- Multi-user/fast-switch correctness; audit log + gap surfacing.
- **Exit:** AC-20, AC-21, AC-32, AC-42, AC-53 pass.

## Phase 4 — Web activity & devices (optional, opt-in) (2–3 weeks)
- Firefox/Chromium history import (domain-only default).
- udev device events.
- Dashboard web/device views. High-sensitivity defaults off.
- **Exit:** AC-70/71 and web-device acceptance tests pass.

## Phase 5 — Hardening & packaging polish
- systemd hardening, encryption-at-rest option, TOTP dashboard auth.
- Upgrade/migration and purge flows; soak test.
- **Exit:** AC-40/41/50/51/52/54/60/61/62 pass.

## Phase 6 — Enforcement (optional, later)
- Time budgets, bedtimes, app/domain rules with visible messaging.
- **Exit:** enforcement tests pass; collection still works when disabled.

## Phase 7 — Nice-to-haves
- Optional multi-machine sync (explicit, encrypted).
- Screenshots (opt-in, disclosed, short retention).
- Search-term capture (opt-in, high sensitivity).
- Localisation.

## Prioritisation rationale

Value order: **screen time → which apps → reports/alerts → web → enforcement**.
Each phase answers a real parent question. Wayland app tracking is the biggest
technical risk and is isolated to Phase 2 so earlier value isn't blocked by it.
