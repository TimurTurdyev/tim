package ui

import (
	"strconv"
	"strings"
	"time"
)

// spanUnits: дни и недели time.ParseDuration не знает.
var spanUnits = []struct {
	suffix string
	unit   time.Duration
}{{"d", 24 * time.Hour}, {"w", 7 * 24 * time.Hour}}

// ParseSpan: длительность, как её пишут руками - 30s, 15m, 2h, 3d, 1w.
func ParseSpan(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	for _, u := range spanUnits {
		if n, ok := strings.CutSuffix(s, u.suffix); ok {
			v, err := strconv.Atoi(n)
			if err != nil || v <= 0 {
				return 0, Errorf("срок %s: нужно вроде 30m, 24h, 3d или 1w", s)
			}
			return time.Duration(v) * u.unit, nil
		}
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, Errorf("срок %s: нужно вроде 30m, 24h, 3d или 1w", s)
	}
	return d, nil
}

// ParseSince: момент "с которого" - дата 2025-10-01 или срок назад от now (3d, 1w).
func ParseSince(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if t, err := time.ParseInLocation("2006-01-02", s, now.Location()); err == nil {
		return t, nil
	}
	d, err := ParseSpan(s)
	if err != nil {
		return time.Time{}, Errorf("с какого момента: дата 2025-10-01 или срок вроде 3d, 1w: %s", s)
	}
	return now.Add(-d), nil
}
