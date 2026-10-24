// Package model holds the domain types shared by the daemon, the agent, the
// dashboard API and the CLI. All instants are Unix milliseconds in UTC.
package model

// State is what a monitored user's session is doing at an instant.
type State string

const (
	StateActive    State = "active"
	StateIdle      State = "idle"
	StateLocked    State = "locked"
	StateScreenOff State = "screen_off"
	StateSuspended State = "suspended"
	// StateLoggedOut never appears as a stored interval: the absence of an
	// interval means "not signed in, suspended or powered off" (docs/05 §5.1).
	StateLoggedOut State = "logged_out"
)

// IntervalStates are the states that are stored as activity intervals.
var IntervalStates = []State{StateActive, StateIdle, StateLocked, StateScreenOff, StateSuspended}

// Valid reports whether s is a state that can be stored as an interval.
func (s State) Valid() bool {
	for _, v := range IntervalStates {
		if s == v {
			return true
		}
	}
	return false
}

// SessionType is the logind session type (docs/04 §4.13.3).
type SessionType string

const (
	SessionWayland SessionType = "wayland"
	SessionX11     SessionType = "x11"
	SessionTTY     SessionType = "tty"
	SessionUnknown SessionType = "unknown"
)

// Provider is a foreground-app provider, chosen per session.
type Provider string

const (
	ProviderNone         Provider = "none"
	ProviderX11          Provider = "x11"
	ProviderGnomeWayland Provider = "gnome-wayland"
)

// AppStatus describes foreground-app tracking for a user's current session.
type AppStatus string

const (
	AppOK       AppStatus = "ok"
	AppDegraded AppStatus = "degraded"
	AppOff      AppStatus = "off"
)

// Interval is a contiguous period of one state for one user.
type Interval struct {
	ID    int64 `json:"id"`
	UID   int   `json:"uid"`
	State State `json:"state"`
	Start int64 `json:"started_at"`
	End   int64 `json:"ended_at"`
	Open  bool  `json:"open"`
}

// Duration returns the interval length, never negative.
func (i Interval) Duration() int64 {
	if i.End < i.Start {
		return 0
	}
	return i.End - i.Start
}

// Sample is one foreground-app observation.
type Sample struct {
	ID     int64  `json:"id"`
	UID    int    `json:"uid"`
	TS     int64  `json:"ts"`
	AppID  string `json:"app_id"`
	Title  string `json:"window_title,omitempty"`
	PID    int    `json:"pid"`
	State  State  `json:"state"`
	Source string `json:"source"`
}

// Gap is a period during which the service was not recording.
type Gap struct {
	ID    int64  `json:"id"`
	UID   int    `json:"uid"` // 0 means "all users"
	Start int64  `json:"started_at"`
	End   int64  `json:"ended_at"`
	Kind  string `json:"kind"`
}

// Gap kinds. All of these count towards an hour's gap_ms; app-sensor
// gaps affect app data only (docs/04 §4.12).
const (
	GapServiceStopped = "service_stopped"
	GapCrash          = "crash"
	GapUnclean        = "unclean_session"
	GapStall          = "stall"
	// GapAppSensor is a period without foreground-app data (agent or extension
	// lost). It is not a tracking gap: screen time kept being recorded.
	GapAppSensor = "app_sensor"
)

// CountsAsTrackingGap reports whether a gap kind is a tracking gap in the
// hourly views.
func CountsAsTrackingGap(kind string) bool {
	switch kind {
	case GapServiceStopped, GapCrash, GapUnclean, GapStall:
		return true
	}
	return false
}

// Audit actions (docs/05 audit_log).
const (
	AuditServiceStarted    = "service_started"
	AuditServiceStopped    = "service_stopped"
	AuditConfigChanged     = "config_changed"
	AuditUsersChanged      = "users_changed"
	AuditCollectorDisabled = "collector_disabled"
	AuditDataDeleted       = "data_deleted"
	AuditSensorLost        = "app_sensor_lost"
	AuditSensorRestored    = "app_sensor_restored"
	AuditSensorUnavailable = "app_sensor_unavailable"
	AuditClockSkew         = "clock_skew"
	AuditDisclosureAck     = "disclosure_acknowledged"
	AuditSetup             = "setup"
	// AuditError and AuditWarning are what the service itself logged as an
	// error or a warning (a failed rollup, a socket that stopped, ...).
	AuditError   = "error"
	AuditWarning = "warning"
)

// Audit levels, as shown and filtered in the dashboard's Audit log.
const (
	LevelError   = "error"
	LevelWarning = "warning"
	LevelInfo    = "info"
)

// AuditLevel is how serious an audit action is. A sensor that cannot read the
// foreground application is an error: the parent is not getting the data they
// expect.
func AuditLevel(action string) string {
	switch action {
	case AuditError, AuditSensorLost, AuditSensorUnavailable:
		return LevelError
	case AuditWarning, AuditClockSkew, AuditCollectorDisabled, AuditDataDeleted:
		return LevelWarning
	}
	return LevelInfo
}

// AuditEvent is one audit_log row.
type AuditEvent struct {
	ID     int64  `json:"id"`
	TS     int64  `json:"ts"`
	Actor  string `json:"actor"`
	Action string `json:"action"`
	Detail string `json:"detail"`
}

// Alert is one alerts row.
type Alert struct {
	ID           int64  `json:"id"`
	CreatedAt    int64  `json:"created_at"`
	RuleID       string `json:"rule_id"`
	UID          int    `json:"uid"`
	Severity     string `json:"severity"`
	Message      string `json:"message"`
	Delivered    bool   `json:"delivered"`
	DeliveredVia string `json:"delivered_via"`
}

// User is a local account known to the tracker.
type User struct {
	UID       int    `json:"uid"`
	Username  string `json:"username"`
	Monitored bool   `json:"monitored"`
	AddedAt   int64  `json:"added_at"`
}

// Session is one logind session of a monitored user.
type Session struct {
	ID          int64       `json:"id"`
	UID         int         `json:"uid"`
	LogindID    string      `json:"logind_session_id"`
	StartedAt   int64       `json:"started_at"`
	EndedAt     int64       `json:"ended_at"` // 0 while open
	State       State       `json:"state"`
	HostType    string      `json:"host_type"`
	SessionType SessionType `json:"session_type"`
	Desktop     string      `json:"desktop"`
	EndReason   string      `json:"end_reason"`
}

// HourRow is the stored per-hour aggregate (docs/05 hourly_usage).
type HourRow struct {
	UID       int    `json:"uid"`
	HourStart int64  `json:"hour_start"`
	Day       string `json:"day"`
	Hour      int    `json:"hour"`
	HourLenMs int64  `json:"hour_len_ms"`
	ActiveMs  int64  `json:"active_ms"`
	IdleMs    int64  `json:"idle_ms"`
	LockedMs  int64  `json:"locked_ms"`
	GapMs     int64  `json:"gap_ms"`
}

// HourAppRow is the stored per-hour per-app aggregate.
type HourAppRow struct {
	UID          int    `json:"uid"`
	HourStart    int64  `json:"hour_start"`
	AppID        string `json:"app_id"`
	AppName      string `json:"app_name"`
	Category     string `json:"category"`
	ForegroundMs int64  `json:"foreground_ms"`
}

// AppDay is the stored per-day per-app aggregate (docs/05 app_usage).
type AppDay struct {
	UID          int    `json:"uid"`
	Day          string `json:"day"`
	AppID        string `json:"app_id"`
	AppName      string `json:"app_name"`
	Category     string `json:"category"`
	ForegroundMs int64  `json:"foreground_ms"`
	OpenMs       int64  `json:"open_ms"`
	SessionCount int64  `json:"session_count"`
}
