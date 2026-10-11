# 03 – Non-Functional Requirements

**Priority:** `MUST` (MVP), `SHOULD` (target), `MAY` (nice to have).

## 3.1 Performance & resource usage

| ID | Priority | Requirement |
| --- | --- | --- |
| NFR-1.1 | MUST | Steady-state CPU usage < **1%** of a single core on a typical desktop. |
| NFR-1.2 | MUST | Resident memory < **100 MB** total across the service and all running agents. |
| NFR-1.3 | MUST | Idle disk I/O is negligible; writes are batched (not one fsync per sample). |
| NFR-1.4 | MUST | Must not cause visible UI latency or input lag on the monitored session. |
| NFR-1.5 | SHOULD | Sampling interval is configurable (default 5 s; range 1–60 s). |
| NFR-1.6 | SHOULD | Dashboard page loads in < 1 s for 30 days of data on commodity hardware. |
| NFR-1.7 | MAY | Battery impact acceptable on laptops (wakeup batching). |
| NFR-1.8 | MUST | The live view adds no meaningful load: the daemon publishes state from memory (no extra database read per client per tick); one open dashboard costs < 0.1% of a core and a few KB/s. Total remains within NFR-1.1/1.2. |
| NFR-1.9 | MUST | Live push must never block or slow the sampling/write path; a slow or stuck client is dropped, not waited for. |
| NFR-1.10 | MUST | The number of simultaneous live connections is bounded (default 10); excess clients get a clear "too many live viewers" response and fall back to polling. |
| NFR-1.11 | SHOULD | Hourly history queries (one day, or a 30-day heatmap) return in < 300 ms on commodity hardware; they read hourly aggregates, not raw samples. |

## 3.2 Reliability & durability

| ID | Priority | Requirement |
| --- | --- | --- |
| NFR-2.1 | MUST | No data loss on unclean shutdown: use WAL/atomic writes or bounded in-memory buffer with flush-on-exit. |
| NFR-2.2 | MUST | Collector failure in one subsystem must not crash the whole service (isolation). |
| NFR-2.3 | MUST | On restart, sessions in progress are closed cleanly and gaps marked. |
| NFR-2.4 | SHOULD | Clock changes / NTP jumps handled without negative-duration records. |
| NFR-2.5 | SHOULD | Runs for weeks without memory growth (no leaks). |
| NFR-2.6 | MAY | Health watchdog restarts a hung service. |

## 3.3 Compatibility

| ID | Priority | Requirement |
| --- | --- | --- |
| NFR-3.1 | MUST | **Ubuntu 24.04 LTS and newer only.** Ubuntu 24.04 LTS is the oldest supported release; later LTS and interim releases are supported as they ship. **Ubuntu 22.04 LTS and anything older are explicitly unsupported** — not built for, not tested, no fixes. |
| NFR-3.2 | MUST | GNOME desktop supported (GNOME Shell 46 or newer, the 24.04 baseline). |
| NFR-3.3 | MUST | **Both Wayland and X11 graphical sessions are supported, and neither may be dropped.** Wayland is mandatory on every supported release (default session on 24.04; the only GNOME session from Ubuntu 25.10). X11 is mandatory wherever the release or flavour offers an X11 session (24.04's "Ubuntu on Xorg", X11-only flavours such as Xubuntu). Capabilities and degradation for each are documented in [04 §4.13](04-architecture.md#413-session-types-wayland-and-x11). A machine may be used in either session type, and different monitored users may be in different session types at the same time. |
| NFR-3.4 | SHOULD | KDE Plasma / XFCE supported at a basic level (session/app tracking) on official Ubuntu flavours built on 24.04 or newer; untested DEs must fail gracefully. |
| NFR-3.5 | MUST | x86_64 and arm64 architectures. |
| NFR-3.6 | SHOULD | No hard dependency on deprecated stack (e.g., works without X11 tooling on Wayland). |
| NFR-3.7 | MUST | The code may assume the 24.04 baseline — systemd 255 and glibc 2.39 — and carries **no compatibility shims** for older releases. |
| NFR-3.8 | MUST | An unsupported OS fails **early and clearly**: `apt` refuses to install the package on a release older than 24.04 (versioned dependencies, NFR-6.8), and `parent-tracker doctor` reports "unsupported OS" with the upgrade hint if the binaries are run anyway. |
| NFR-3.9 | MUST | Screen-time correctness (active / idle / locked, sessions, gaps) is **independent of the session type**: the same scripted session produces the same numbers on Wayland and on X11. |
| NFR-3.10 | MUST | The absence of an X11 session on a release is **not an error**: `doctor` reports it as expected and no X11 tooling is required on such systems. Conversely, an unavailable Wayland extension is a *degraded* state with a visible banner, never a silent one. |

## 3.4 Security

| ID | Priority | Requirement |
| --- | --- | --- |
| NFR-4.1 | MUST | Runs with least privilege; a dedicated system user where feasible, root only where strictly required (e.g., reading other users' browser history). |
| NFR-4.2 | MUST | Data files are readable only by root/admins (mode `0600`/`0640`, owned by the service user). |
| NFR-4.3 | MUST | Dashboard is bound to loopback by default and requires authentication. |
| NFR-4.4 | MUST | No sensitive data in world-readable logs; titles/URLs redactable. |
| NFR-4.5 | SHOULD | Configurable encryption at rest for the database/screenshots. |
| NFR-4.6 | MUST | systemd hardening directives (`NoNewPrivileges`, `ProtectSystem`, etc.) applied where compatible. |
| NFR-4.7 | SHOULD | Tamper-evidence: monitored user cannot silently disable tracking; an audit event is logged and surfaced to the parent. (Not tamper-*proofing* against a determined admin.) |

## 3.5 Privacy

| ID | Priority | Requirement |
| --- | --- | --- |
| NFR-5.1 | MUST | Data stays on-device unless the admin explicitly enables sync/export. |
| NFR-5.2 | MUST | No analytics/telemetry phone-home ever. This includes the dashboard itself: it loads no scripts, styles, fonts or icons from any other origin (see [12 – Design System: Lumen](12-design-system-lumen.md), DS-9). |
| NFR-5.3 | MUST | Collection is scoped to allowlisted users only; no capture of other accounts. |
| NFR-5.4 | SHOULD | The monitored child can view their collected data and knows tracking is on. |
| NFR-5.5 | MUST | High-sensitivity collectors (screenshots, full URLs, search terms) are off by default. |

## 3.6 Maintainability & packaging

| ID | Priority | Requirement |
| --- | --- | --- |
| NFR-6.1 | MUST | Distributed as a `.deb` package; installable/upgradable via `apt`/`dpkg`. |
| NFR-6.2 | MUST | Package handles systemd unit install/enable and config file placement (no clobbering admin config on upgrade). |
| NFR-6.3 | SHOULD | Clean uninstall/purge that removes service, files, and (optionally) data. |
| NFR-6.4 | SHOULD | Migration of the database schema on version upgrade, with backup. |
| NFR-6.5 | MUST | Open configuration format (TOML/JSON) with documented schema. |
| NFR-6.6 | SHOULD | Source builds reproducibly; CI runs unit/integration tests. |
| NFR-6.7 | MAY | Auto-update channel / signed releases. |
| NFR-6.8 | MUST | The `.deb` declares versioned dependencies matching the baseline (`systemd (>= 255)`, and `libc6 (>= 2.39)` if dynamically linked) so that installation on Ubuntu 22.04 or older fails at the `apt`/`dpkg` dependency check, before anything is installed or enabled. |
| NFR-6.9 | MUST | Schema migrations that introduce hourly history (new tables) are additive and **backfill** from existing intervals/samples where they still exist, without losing or altering existing data. |
| NFR-6.10 | MUST | Packages are built for **both amd64 and arm64** (NFR-3.5); the build and docs name the architecture explicitly in the file name. |
| NFR-6.11 | MUST | The package ships the licences and notices of bundled third-party material (Lumen/Spectrum-derived values, Lucide, React, the fonts) in `/usr/share/doc/parent-tracker/copyright`. |

## 3.7 Observability & logging

| ID | Priority | Requirement |
| --- | --- | --- |
| NFR-7.1 | MUST | Structured logs to journald with configurable level. |
| NFR-7.2 | MUST | Log rotation / journald rate limits respected; no unbounded disk growth. |
| NFR-7.3 | SHOULD | `parent-tracker status` prints current sessions, last sample, record counts. |
| NFR-7.4 | SHOULD | Disk-space guard: stop collection or prune when storage is low, and alert. |

## 3.8 Usability & accessibility

| ID | Priority | Requirement |
| --- | --- | --- |
| NFR-8.1 | MUST | Dashboard responsive enough for tablet/phone on the LAN. |
| NFR-8.2 | SHOULD | Dashboard is keyboard accessible and screen-reader friendly. Known contrast limits inherited from Lumen's field borders are listed in [12 §12.8](12-design-system-lumen.md#128-known-lumen-limitations-and-how-the-project-handles-them). |
| NFR-8.3 | SHOULD | Clear language; avoid shaming tone in reports shown to the child. The child's activity is never presented with "good/bad" status styling. |
| NFR-8.4 | MAY | Localisation infrastructure (English first). |
| NFR-8.5 | MUST | **The dashboard UI is built with the Lumen design system**: Lumen tokens and components only, no second UI kit or icon set, no hard-coded colours/sizes/radii. Full rules: [12 – Design System: Lumen](12-design-system-lumen.md). |
| NFR-8.6 | MUST | All dashboard assets (Lumen files, React 18, fonts, icons) are vendored and served by the daemon; no request leaves the origin. Lumen ships no font files, so Source Sans 3 and Source Code Pro are bundled by this project. |
| NFR-8.7 | MUST | Light and dark themes (following the OS by default); `prefers-reduced-motion` honoured; status never conveyed by colour alone. |
| NFR-8.8 | SHOULD | Phone/tablet usability despite Lumen's desktop-only scale: responsive layout and larger control sizes on narrow screens. |

## 3.9 Legal / compliance

| ID | Priority | Requirement |
| --- | --- | --- |
| NFR-9.1 | MUST | Ship a disclosure notice and a first-run acknowledgement screen. |
| NFR-9.2 | MUST | Documented data-retention and deletion controls (supports GDPR/COPPA-style expectations). |
| NFR-9.3 | SHOULD | Age-appropriate explainer text for the child. |
