package conv

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

var layouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
	"02.01.2006 15:04:05",
	"02.01.2006 15:04",
	"02.01.2006",
}

// ParseTime: epoch в секундах или миллисекундах (по длине) либо дата в одном из форматов в зоне loc.
func ParseTime(s string, loc *time.Location) (time.Time, error) {
	t := strings.TrimSpace(s)
	if n, err := strconv.ParseInt(t, 10, 64); err == nil {
		switch {
		case len(strings.TrimPrefix(t, "-")) >= 16:
			return time.UnixMicro(n).In(loc), nil
		case len(strings.TrimPrefix(t, "-")) >= 13:
			return time.UnixMilli(n).In(loc), nil
		}
		return time.Unix(n, 0).In(loc), nil
	}
	for _, l := range layouts {
		if v, err := time.ParseInLocation(l, t, loc); err == nil {
			return v, nil
		}
	}
	return time.Time{}, errors.New("не понял время: " + s)
}
