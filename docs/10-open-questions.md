# 10 – Open Questions & Assumptions

These are decisions still to be made. Each has a **recommendation** and an
**impact** so we can move forward quickly.

## Assumptions currently made

| # | Assumption | If wrong, what changes |
| --- | --- | --- |
| A1 | Single family PC, **Ubuntu 24.04 or newer**, GNOME. (22.04 and older are explicitly unsupported.) | Compatibility and collector work expands; supporting 22.04 again would need GNOME 42-era Shell/systemd 249 fallbacks. |
| A2 | Parent has root/sudo on the PC and owns it. | Legal/ethical basis changes; project may be inappropriate. |
| A3 | Child is a minor; tracking is disclosed, not covert. | Privacy design (self-view, disclosure) would be dropped — not recommended. |
| A4 | "Usage" = active, unlocked, non-idle screen time. | Definition of core metric changes. |
| A5 | Local-only data is acceptable for MVP. | Sync/back-end work moves earlier. |
| A6 | English-only for now. | i18n effort. |
| A7 | One machine for MVP. | Multi-machine architecture needed sooner. |

## Open questions

1. **Language / stack** — Rust vs Go (both fine) vs Python prototype.
   *Recommendation:* Rust or Go for the daemon; ship a Python prototype only if
   speed of iteration matters more than footprint.
2. **Wayland app tracking route** — GNOME Shell extension vs. relying on
   degraded signals.
   *Recommendation:* extension for GNOME (primary), degrade elsewhere; document
   clearly. This is the largest technical risk.
3. **Alert channel** — email vs ntfy vs Telegram/Discord webhook.
   *Recommendation:* ntfy (simple, self-hostable, no SMTP creds) plus desktop
   notifications; email later.
4. **Dashboard exposure** — loopback only, or LAN with auth?
   *Recommendation:* loopback default; LAN optional with auth + warning.
5. **Enforcement scope** — monitoring only, or also time limits/blocking?
   *Recommendation:* monitoring only until Phases 1–3 are solid; enforcement is
   a separate opt-in phase.
6. **Screenshots** — include at all?
   *Recommendation:* defer; if included, opt-in, disclosed, 7-day retention,
   encrypted.
7. **Web history depth** — domain only or full URLs (and search terms)?
   *Recommendation:* domain only by default; full URL/search as explicit,
   clearly-indicated opt-ins.
8. **At-rest encryption** — always-on or optional?
   *Recommendation:* optional in MVP (key management friction), default-on by
   Phase 5.
9. **Tamper resistance level** — evidence vs. hard enforcement.
   *Recommendation:* evidence + audit log; document that root can always
   disable.
10. **Distribution** — private `.deb` vs public repo.
    *Recommendation:* build `.deb` locally first; decide on hosting later.
11. **Child self-view** — in MVP or later?
    *Recommendation:* minimal read-only self-view early (it's cheap and central
    to the ethics of the project).
12. **Data volume / retention defaults** — 30 vs 90 days raw.
    *Recommendation:* 30 days raw, aggregates longer; configurable.
13. **Hourly history retention** — 30 days (like raw samples), 1 year, or 2 years
    (like daily app aggregates)?
    *Recommendation:* **1 year** (`retention.hourly_usage`). Long enough to see
    school-term vs. holiday patterns; shorter than daily aggregates because an
    hour-by-hour pattern is more revealing than a daily total (least collection).
14. **Wayland-only newer releases** — Ubuntu 25.10+ ships GNOME without an X11
    session, so the X11 `xprop` path covers less over time.
    *Recommendation:* treat the Wayland GNOME helper (Q2) as the top technical
    priority; live and hourly views must degrade cleanly without per-app data.

## Next steps to unblock implementation

1. Answer Q1 (stack) and Q2 (Wayland route).
2. Freeze the MVP definition to **Phase 1 + Phase 2**.
3. Pick default alert channel (Q3) and dashboard exposure (Q4).
4. Stand up the repo skeleton and CI (Phase 0).

## Decision log

| Date | Decision | Rationale |
| --- | --- | --- |
| 2026-10-09 | Docs created; scope = monitoring (not covert, not enforcement-first). | Transparency-first stance; enforcement deferred. |
| 2026-10-11 | **Supported platform is Ubuntu 24.04 and newer only.** 22.04 and older are unsupported; the package must refuse to install there (NFR-6.8). | One baseline (systemd 255, glibc 2.39, GNOME Shell 46) removes legacy shims and halves the test matrix. |
| 2026-10-11 | **Real-time dashboard** added to scope: in-memory live hub, Server-Sent Events with polling fallback, status-only (no screen content). | SSE is one-way, needs no extra dependency, reconnects automatically; WebSockets add complexity for no benefit here. Keeps the "no screen streaming" non-goal intact. |
| 2026-10-11 | **Per-hour history** added to scope: `hourly_usage` / `hourly_app_usage`, 1-year default retention, shown in dashboard, CLI, export and the child's self-view. | Answers "when", not just "how much"; stored as aggregates so it outlives raw samples; child sees everything the parent sees (SEC-43). |
