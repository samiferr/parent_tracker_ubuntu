# 05 – Data Model

Storage is SQLite (WAL). Types below are logical; exact SQL in implementation.
All timestamps are UTC with millisecond precision; user-facing display uses
local time.

## 5.1 Core entities

### `users`
| Column | Type | Notes |
| --- | --- | --- |
| uid | INTEGER PK | Local UID |
| username | TEXT | |
| monitored | INTEGER (bool) | 1 = tracked; 0 = present but untracked |
| added_at | TIMESTAMP | |

### `sessions`
| Column | Type | Notes |
| --- | --- | --- |
| id | INTEGER PK | |
| uid | INTEGER FK users | |
| started_at | TIMESTAMP | login/unlock |
| ended_at | TIMESTAMP nullable | logout/lock |
| state | TEXT | active / idle / locked / screen_off / suspended / logged_out |
| host_type | TEXT | local / remote(ssh) |
| wayland_or_x11 | TEXT | |
| end_reason | TEXT | normal / unclean / clock_skew |

### `activity_intervals`
Derived contiguous periods of a single state for a user (source of truth for
screen time). Reduces raw sample volume.
| Column | Type | Notes |
| --- | --- | --- |
| id | INTEGER PK | |
| uid | INTEGER | |
| state | TEXT | active/idle/locked/screen_off |
| started_at | TIMESTAMP | |
| ended_at | TIMESTAMP | |
| duration_ms | INTEGER | computed |

### `app_usage` (aggregate, per day)
| Column | Type | Notes |
| --- | --- | --- |
| uid | INTEGER | |
| day | DATE | local day |
| app_id | TEXT | desktop id / exe basename |
| app_name | TEXT | human readable |
| category | TEXT | rules-derived |
| foreground_ms | INTEGER | time in foreground & active |
| open_ms | INTEGER | app running while user active |
| session_count | INTEGER | |
| PK | (uid, day, app_id) | |

### `window_samples` (raw, retention-limited)
| Column | Type | Notes |
| --- | --- | --- |
| id | INTEGER PK | |
| uid | INTEGER | |
| ts | TIMESTAMP | |
| app_id | TEXT | |
| window_title | TEXT nullable | optional, can be disabled |
| pid | INTEGER | |
| state | TEXT | active/idle/locked |

Index: `(uid, ts)`, `(uid, app_id, ts)`.

### `web_visits` (phase 2, opt-in)
| Column | Type | Notes |
| --- | --- | --- |
| id | INTEGER PK | |
| uid | INTEGER | |
| ts | TIMESTAMP | |
| browser | TEXT | firefox / chrome |
| domain | TEXT | default granularity |
| url | TEXT nullable | only if full-URL capture enabled |
| title | TEXT nullable | |

### `device_events` (phase 2)
| Column | Type | Notes |
| --- | --- | --- |
| id | INTEGER PK | |
| ts | TIMESTAMP | |
| action | TEXT | add / remove |
| bus | TEXT | usb / bluetooth / … |
| description | TEXT | e.g. "SanDisk USB drive" |

### `alerts`
| Column | Type | Notes |
| --- | --- | --- |
| id | INTEGER PK | |
| created_at | TIMESTAMP | |
| rule_id | TEXT | |
| uid | INTEGER nullable | |
| severity | TEXT | info / warn / critical |
| message | TEXT | |
| delivered | INTEGER (bool) | |
| delivered_via | TEXT | ntfy / email / desktop |

### `audit_log`
Tamper-evidence and admin actions.
| Column | Type | Notes |
| --- | --- | --- |
| id | INTEGER PK | |
| ts | TIMESTAMP | |
| actor | TEXT | system / admin-uid |
| action | TEXT | service_stopped, config_changed, collector_disabled, data_deleted, … |
| detail | TEXT (JSON) | |

### `schema_migrations`
| version | INTEGER PK | applied_at | TIMESTAMP |

### `collector_health`
| collector | TEXT PK | last_ok_at | last_error | enabled |

## 5.2 Retention policy

| Data | Default retention | After expiry |
| --- | --- | --- |
| Raw `window_samples` | 30 days | deleted (aggregates kept) |
| `activity_intervals` | 180 days | deleted |
| `app_usage` aggregates | 2 years | deleted |
| `web_visits` | 90 days | deleted |
| Screenshots (phase 3) | 7 days | deleted |
| `alerts` / `audit_log` | 1 year | deleted |
| `sessions` | 1 year | deleted |

Retention is configurable per category. Deletion is a real `DELETE` + `VACUUM`
(or incremental vacuum) so data is not recoverable from the file.

## 5.3 Derived calculations

- **Active time per day** = `SUM(duration_ms)` over `activity_intervals` where
  state = `active` within local day boundaries.
- **App foreground time** = sum of sample deltas attributed to the app while
  state = `active`; sample interval capped so a stale sample doesn't over-count.
- **Idle ≠ usage**: any interval in `idle`/`locked`/`screen_off` is excluded.
- Time overlapping a clock change is clamped (no negative, no double count).

## 5.4 Data volume estimate

At 5 s sampling, ~17,280 samples/day/user ≈ ~2–4 MB/day raw depending on title
storage. With 90-day raw retention, < ~400 MB per user is expected — acceptable
for local disk; aggregator keeps this bounded.

## 5.5 Export format

JSONL for events, CSV for aggregates. Export bundle includes a manifest with
schema version, date range, and users, plus a checksum.
