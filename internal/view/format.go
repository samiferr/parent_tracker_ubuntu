package view

import "fmt"

// FmtHM formats milliseconds as "1h 12m", "42m" or "0m" (CLI style).
func FmtHM(ms int64) string {
	m := (ms + 30_000) / 60_000
	if m >= 60 {
		if m%60 == 0 {
			return fmt.Sprintf("%dh", m/60)
		}
		return fmt.Sprintf("%dh %02dm", m/60, m%60)
	}
	return fmt.Sprintf("%dm", m)
}

// FmtHMSpaced formats milliseconds as "3 h 05 m" or "42 m" (dashboard summary
// line, as in docs/07 §7.2.8).
func FmtHMSpaced(ms int64) string {
	m := (ms + 30_000) / 60_000
	if m >= 60 {
		return fmt.Sprintf("%d h %02d m", m/60, m%60)
	}
	return fmt.Sprintf("%d m", m)
}
