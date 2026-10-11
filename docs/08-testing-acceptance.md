# 08 – Testing & Acceptance

## 8.1 Test levels

| Level | What it covers |
| --- | --- |
| Unit | Aggregation math (active vs idle), retention pruning, rule evaluation, config parsing, app mapping. |
| Integration | Collectors against recorded fixtures (logind signals, X11/Wayland window samples, browser DB snapshots). |
| End-to-end | Real VM: install → monitor a session → see correct numbers in dashboard. |
| Performance | CPU/RAM/disk under 24 h simulated workload; dashboard query latency. |
| Security | File permissions, socket peer auth, dashboard auth, no secret leakage in logs. |
| Packaging | install/upgrade/uninstall on clean 22.04 and 24.04, X11 and Wayland. |
| Compatibility | GNOME primary; KDE/XFCE smoke tests. |

## 8.2 Key acceptance criteria

### A. Session & screen time
- **AC-1** Logging in starts a session row; logging out closes it.
- **AC-2** Locking the screen changes state to `locked` within one sample
  interval; active time stops accruing.
- **AC-3** 30 minutes with the screen locked adds **0** active minutes.
- **AC-4** 60 minutes of genuine active use reports 57–63 minutes active.
- **AC-5** Suspend/resume across 2 hours shows a `suspended` interval, not 2 h
  of active time.
- **AC-6** A system clock jump does not produce negative durations.

### B. Application tracking
- **AC-10** With two apps used 10 and 20 minutes foreground, the app breakdown
  reports approximately those values (±10%).
- **AC-11** An app left open but idle all day reports low foreground time.
- **AC-12** On Wayland without the shell extension, the dashboard shows a
  degraded-app-tracking banner and no crash.
- **AC-13** Window titles are absent from storage and logs when title capture
  is disabled.

### C. Multi-user
- **AC-20** Only allowlisted users are recorded; a non-monitored account
  produces zero rows.
- **AC-21** Fast user switching attributes time to the correct user.

### D. Reporting
- **AC-30** A parent can determine daily active time and top apps from the
  dashboard without any other tooling.
- **AC-31** Date-range selection returns aggregates consistent with raw data.
- **AC-32** CSV/PDF export matches dashboard figures.

### E. Reliability
- **AC-40** `kill -9` the daemon mid-session; after restart, prior data is
  intact and the interrupted session is closed with a gap marker.
- **AC-41** Pull the plug / hard reboot; no corrupt DB; service auto-starts.
- **AC-42** 7-day soak shows stable memory and no unbounded DB growth.

### F. Security & privacy
- **AC-50** DB/config are not readable by the monitored user.
- **AC-51** Dashboard is unreachable from another LAN host by default.
- **AC-52** Dashboard requires auth; unauthenticated requests are rejected.
- **AC-53** Stopping the service creates a visible "gap"/audit entry in the
  parent's view.
- **AC-54** No credentials appear in logs.

### G. Packaging
- **AC-60** `apt install ./parent-tracker.deb` on a clean 22.04 and 24.04 sets
  up and enables the service.
- **AC-61** Upgrade preserves `/etc` config and migrates the DB.
- **AC-62** `apt purge` removes the service and binaries; data removal is a
  separate confirmed action.

### H. Privacy defaults
- **AC-70** Screenshots, full-URL capture, and search-term capture are all off
  on a default install.
- **AC-71** First run requires explicit acknowledgement of the disclosure.

## 8.3 Test fixtures & harness

- A "fake session" harness that replays scripted logind/lock/idle transitions
  so tests are deterministic without a real desktop.
- Captured X11 and Wayland signal traces used as golden inputs.
- A disposable VM image with both an X11 and a Wayland session for E2E.
- Ground-truth script for AC-4/AC-10 that logs actual foreground windows
  independently and compares.

## 8.4 Quality gates

- Unit + integration tests pass in CI on 22.04 and 24.04.
- Linting/formatting enforced.
- No new P1/P2 security finding open.
- Resource budget check (NFR-1.1/1.2) passes in the performance test.

## 8.5 Manual test matrix

| Dimension | Values |
| --- | --- |
| Ubuntu | 22.04, 24.04 |
| Session | X11, Wayland |
| DE | GNOME (primary), KDE (smoke) |
| Arch | x86_64, arm64 |
| Scenario | single user, fast-switch, suspend/resume, hard reboot, disk-full |
