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
| logind_session_id | TEXT | logind's session id (e.g. `2`), for correlation |
| started_at | TIMESTAMP | login/unlock |
| ended_at | TIMESTAMP nullable | logout/lock |
| state | TEXT | active / idle / locked / screen_off / suspended / logged_out |
| host_type | TEXT | local / remote(ssh) |
| session_type | TEXT | `wayland` / `x11` / `tty` / `unknown`, from logind `Type` (see [04 §4.13](04-architecture.md#413-session-types-wayland-and-x11)) |
| desktop | TEXT nullable | e.g. `GNOME`, from logind `Desktop` |
| end_reason | TEXT | normal / unclean / clock_skew |

### `activity_intervals`
Derived contiguous periods of a single state for a user (source of truth for
screen time). Reduces raw sample volume.
| Column | Type | Notes |
| --- | --- | --- |
| id | INTEGER PK | |
| uid | INTEGER | |
| state | TEXT | active / idle / locked / screen_off / suspended (screen_off is recorded only where a source exists, else folded into idle/locked) |
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

### `hourly_usage` (aggregate, per local hour)
One row per user per completed local hour. Source of truth for the per-hour
history; written by the rollup (see [04 – Architecture](04-architecture.md#412-hourly-rollup)).
| Column | Type | Notes |
| --- | --- | --- |
| uid | INTEGER | |
| hour_start | TIMESTAMP | UTC instant at which the local hour begins (so DST "fall back" yields two distinct rows) |
| day | DATE | local day the hour belongs to |
| hour | INTEGER | local hour of day, 0–23 (label only; not unique on a DST fall-back day) |
| hour_len_ms | INTEGER | normally 3 600 000; shorter/longer only for unusual DST shifts |
| active_ms | INTEGER | time in `active` clipped to this hour |
| idle_ms | INTEGER | time in `idle` clipped to this hour |
| locked_ms | INTEGER | `locked` + `screen_off` clipped to this hour |
| gap_ms | INTEGER | overlap with recorded tracking gaps (service stopped/crashed, unclean session end) |
| PK | (uid, hour_start) | Index `(uid, day)` |

`hour_len_ms − active_ms − idle_ms − locked_ms − gap_ms` is the time the user
was **not signed in, the PC was suspended, or it was powered off**; it is derived
at read time and not stored.
The hour currently in progress is not stored; it is computed on demand.

### `hourly_app_usage` (aggregate, per local hour per app)
| Column | Type | Notes |
| --- | --- | --- |
| uid | INTEGER | |
| hour_start | TIMESTAMP | same meaning as in `hourly_usage` |
| app_id | TEXT | desktop id / exe basename |
| app_name | TEXT | human readable |
| category | TEXT | rules-derived |
| foreground_ms | INTEGER | time in foreground & active within this hour |
| PK | (uid, hour_start, app_id) | Index `(uid, hour_start)` |

Empty (no rows) for hours where app tracking was unavailable, e.g. Wayland
without the GNOME Shell extension, or hours older than `window_samples`
retention at the time the table was first backfilled (only relevant when
upgrading from a release that had no hourly history).

### `window_samples` (raw, retention-limited)
| Column | Type | Notes |
| --- | --- | --- |
| id | INTEGER PK | |
| uid | INTEGER | |
| ts | TIMESTAMP | |
| app_id | TEXT | |
| app_name | TEXT | display name at the time of the sample, so it survives the desktop entry being removed |
| window_title | TEXT nullable | optional, can be disabled |
| pid | INTEGER | |
| state | TEXT | active/idle/locked |
| source | TEXT | `x11` / `gnome-wayland` — which foreground provider produced the sample |

Samples exist only when a foreground provider is working; with none (e.g.
Wayland without the extension) no `window_samples` rows are written and the
state time still comes from `activity_intervals`.

Index: `(uid, ts)`, `(uid, app_id, ts)`.

### `web_visits` (Phase 4, opt-in)
| Column | Type | Notes |
| --- | --- | --- |
| id | INTEGER PK | |
| uid | INTEGER | |
| ts | TIMESTAMP | |
| browser | TEXT | firefox / chrome |
| domain | TEXT | default granularity |
| url | TEXT nullable | only if full-URL capture enabled |
| title | TEXT nullable | |

### `device_events` (Phase 4, opt-in)
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
| delivered_via | TEXT | ntfy / webhook / desktop / email (email only if that channel is added) |

### `audit_log`
Tamper-evidence and admin actions.
| Column | Type | Notes |
| --- | --- | --- |
| id | INTEGER PK | |
| ts | TIMESTAMP | |
| actor | TEXT | system / admin-uid |
| action | TEXT | service_stopped, config_changed, collector_disabled, data_deleted, app_sensor_lost / app_sensor_unavailable / app_sensor_restored (Wayland extension or agent; the detail ends with ` \| ` and the agent's own error text when it sent one), error / warning (what the service logged), … |
| detail | TEXT (JSON) | |

### `schema_migrations`
| version | INTEGER PK | applied_at | TIMESTAMP |

### `collector_health`
| collector | TEXT PK | last_ok_at | last_error | enabled |

One row per collector **and per foreground provider per session**, e.g. `session`,
`web`, `foreground:x11:<session>`, `foreground:gnome-wayland:<session>`; the
Wayland provider's detail (`active` / `missing` / `disabled` / `no-heartbeat`)
goes in `last_error`.

## 5.2 Retention policy

| Data | Default retention | After expiry |
| --- | --- | --- |
| Raw `window_samples` | 30 days | deleted (aggregates kept) |
| `activity_intervals` | 180 days | deleted |
| `app_usage` aggregates | 2 years | deleted |
| `hourly_usage` / `hourly_app_usage` | 1 year (`retention.hourly_usage`) | deleted |
| `web_visits` | 90 days | deleted |
| `device_events` | 90 days | deleted |
| Screenshots (Phase 7) | 7 days | deleted |
| `alerts` / `audit_log` | 1 year (`retention.alerts` governs both) | deleted |
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
- **Hourly state time** = for each `activity_intervals` row, the overlap with
  each local-hour bucket it touches (`min(end, bucket_end) − max(start,
  bucket_start)`), summed per state. An interval is split exactly at hour
  boundaries; the pieces add back up to the original duration.
- **Daily ≡ hourly.** For any local day, `SUM(hourly_usage.active_ms)` equals the
  day's active time from `activity_intervals` (FR-6.22). Both are derived from
  the same intervals, so a mismatch is a bug.
- **Hourly app time** uses the same sample-delta attribution and cap as the
  daily `app_usage` figure, bucketed by the sample's local hour; a sample delta
  that straddles an hour boundary is split at the boundary.
- **Live totals** = stored totals for closed intervals + the open interval up to
  the last good sample (not up to "now"), so a stalled collector cannot inflate
  usage.

## 5.4 Data volume estimate

At 5 s sampling, a full day of samples is ~17,280 rows/user ≈ ~2–4 MB/day raw
depending on title storage (an upper bound: samples exist only while a session
is open and a foreground provider is working). With the default 30-day raw
retention that is roughly 60–120 MB per user at steady state — acceptable for
local disk; the aggregator keeps this bounded. These are estimates to be
measured once code exists (NFR-1.3, AC-42).

Hourly history is small: at most 24 `hourly_usage` rows per user per day
(≈ 9 k rows/year) and a few `hourly_app_usage` rows per active hour, together
well under ~5 MB per user per year at the default 1-year retention.

## 5.5 Export format

`parent-tracker export` writes a single **JSON document** containing a manifest
(schema version, date range, users, checksum) and every dataset, including
`hourly_usage` and `hourly_app_usage` (FR-10.5). `parent-tracker report`
produces CSV for aggregates (hourly CSV has one row per user-hour with the
columns above). JSONL for very large event tables is a later option; there is no
import command in the first release.
