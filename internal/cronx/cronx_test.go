package cronx

import (
	"testing"
	"time"
)

func TestNext(t *testing.T) {
	loc := time.FixedZone("MSK", 3*3600)
	from := time.Date(2025, 10, 18, 20, 50, 30, 0, loc) // суббота
	check := func(expr string, want ...string) {
		t.Helper()
		s, err := Parse(expr)
		if err != nil {
			t.Fatal(err)
		}
		got := s.Next(from, len(want))
		for i, w := range want {
			if i >= len(got) || got[i].Format("2006-01-02 15:04") != w {
				t.Errorf("%s: got %v want %v", expr, got, want)
				return
			}
		}
	}
	check("*/15 * * * *", "2025-10-18 21:00", "2025-10-18 21:15")
	check("30 3 * * *", "2025-10-19 03:30", "2025-10-20 03:30")
	check("0 9 * * mon-fri", "2025-10-20 09:00", "2025-10-21 09:00")
	check("0 0 29 2 *", "2028-02-29 00:00")
	check("0 12 13 * 5", "2025-10-24 12:00", "2025-10-31 12:00", "2025-11-07 12:00")
}
