# 07 – UX & Reporting

## 7.1 Design goals

- A parent gets the key answer (**"how much, and on what?"**) in under a minute.
- No charts-for-charts' sake; default to plain numbers and one clear trend.
- Factual, non-shaming language, especially in the child's self-view.

## 7.2 Dashboard screens

### 7.2.1 Today (default)
- Big numbers: **Active time**, **Idle time**, **First activity**, **Last activity**.
- Top 5 apps as a horizontal bar chart with durations.
- Category donut (games / school / social / video / other) if categories configured.
- Day timeline strip showing active / idle / locked blocks.
- Comparison to 7-day average ("+40 min vs. usual").

### 7.2.2 Trends
- Line/bar of daily active time over 7 / 30 days.
- Stacked per-category trend.
- Bedtime / first-use scatter (when does the day start/end).

### 7.2.3 App detail
- Per-app daily usage, total over range, session count, typical time of day.

### 7.2.4 Web activity (if enabled)
- Top domains, visit counts, time-of-day distribution.
- Clearly labelled as "domain only" unless full-URL mode is on.

### 7.2.5 Alerts & timeline of events
- Rule hits with timestamps; service/tracking gaps highlighted.

### 7.2.6 Settings (admin)
- Monitored users, collectors on/off, retention, thresholds, alert channels,
  dashboard auth, LAN exposure toggle (with warning).

### 7.2.7 Child self-view
- Read-only, same numbers as parent sees for their own account, with an
  explainer of what is collected and how to ask for deletion.

## 7.3 Reports

| Report | Trigger | Content |
| --- | --- | --- |
| Daily digest | Configurable time (default 21:00) | Active time, top apps, notable events, vs. average |
| Weekly summary | Sunday evening | 7-day trend, category split, bedtime pattern |
| On-demand export | CLI / dashboard | CSV/PDF for a date range |

## 7.4 Alerts

| Rule (example) | Default state | Threshold |
| --- | --- | --- |
| Daily active time exceeded | off | e.g. 3 h |
| Usage after bedtime | off | e.g. after 22:00 |
| Specific app opened | off | configurable |
| New USB/Bluetooth device | off | — |
| Tracking gap / service stopped | **on** | any |
| Disk space low | **on** | < 10% |

Channels: local desktop notification, email (SMTP), ntfy/webhook. Rate-limited
and de-duplicated (one alert per rule per window).

## 7.5 CLI UX

```
parent-tracker status                 # current sessions, last sample, counts
parent-tracker users add alice        # start monitoring 'alice'
parent-tracker users remove alice     # stop monitoring
parent-tracker config get/set KEY VAL # edit settings
parent-tracker report --from ... --to ... --format csv
parent-tracker export --out backup.zip
parent-tracker delete --user alice --before 2026-01-01
parent-tracker doctor                 # diagnose capability/permissions
parent-tracker secret set smtp-password
```

## 7.6 First-run flow

1. Install package.
2. `parent-tracker setup` asks: which users to monitor? enable web/screenshots?
   alert channel? sets dashboard password.
3. Show disclosure text and require acknowledgement.
4. Place a visible "activity is recorded" entry in the child's session and (if
   the child is logged in) show a one-time notification.
5. Open dashboard at `http://127.0.0.1:8787`.

## 7.7 Accessibility & i18n

- Keyboard navigable dashboard, sufficient contrast, semantic HTML.
- English default; strings externalised for later translation.

## 7.8 Error & degraded-state UX

- Dashboard banner when a collector is degraded ("App tracking limited on
  Wayland — install the GNOME extension").
- Timeline shows explicit **gaps** rather than drawing straight through them.
- `doctor` prints a checklist with fixes.
