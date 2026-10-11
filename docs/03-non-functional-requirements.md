# 03 – Non-Functional Requirements

**Priority:** `MUST` (MVP), `SHOULD` (target), `MAY` (nice to have).

## 3.1 Performance & resource usage

| ID | Priority | Requirement |
| --- | --- | --- |
| NFR-1.1 | MUST | Steady-state CPU usage < **1%** of a single core on a typical desktop. |
| NFR-1.2 | MUST | Resident memory < **100 MB** total across service + helper. |
| NFR-1.3 | MUST | Idle disk I/O is negligible; writes are batched (not one fsync per sample). |
| NFR-1.4 | MUST | Must not cause visible UI latency or input lag on the monitored session. |
| NFR-1.5 | SHOULD | Sampling interval is configurable (default 5 s; range 1–60 s). |
| NFR-1.6 | SHOULD | Dashboard page loads in < 1 s for 30 days of data on commodity hardware. |
| NFR-1.7 | MAY | Battery impact acceptable on laptops (wakeup batching). |

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
| NFR-3.1 | MUST | Ubuntu **22.04 LTS** and **24.04 LTS** (and current LTS derivatives) supported. |
| NFR-3.2 | MUST | GNOME desktop supported. |
| NFR-3.3 | MUST | **Both X11 and Wayland** sessions supported, with documented capability degradation on each. |
| NFR-3.4 | SHOULD | KDE Plasma / XFCE supported at a basic level (session/app tracking); untested DEs must fail gracefully. |
| NFR-3.5 | MUST | x86_64 and arm64 architectures. |
| NFR-3.6 | SHOULD | No hard dependency on deprecated stack (e.g., works without X11 tooling on Wayland). |

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
| NFR-5.2 | MUST | No analytics/telemetry phone-home ever. |
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
| NFR-8.2 | SHOULD | Dashboard is keyboard accessible and screen-reader friendly. |
| NFR-8.3 | SHOULD | Clear language; avoid shaming tone in reports shown to the child. |
| NFR-8.4 | MAY | Localisation infrastructure (English first). |

## 3.9 Legal / compliance

| ID | Priority | Requirement |
| --- | --- | --- |
| NFR-9.1 | MUST | Ship a disclosure notice and a first-run acknowledgement screen. |
| NFR-9.2 | MUST | Documented data-retention and deletion controls (supports GDPR/COPPA-style expectations). |
| NFR-9.3 | SHOULD | Age-appropriate explainer text for the child. |
