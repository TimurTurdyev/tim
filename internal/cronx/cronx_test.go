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

func TestDescribe(t *testing.T) {
	cases := map[string]string{
		"*/15 9-18 * * mon-fri": "каждые 15 минут с 09:00 до 18:45, по будням",
		"30 3 * * *":            "в 03:30, каждый день",
		"0 */2 * * *":           "каждые 2 ч в :00, каждый день",
		"5 * * * 0,6":           "каждый час в :05, по выходным",
		"0 0 1 * *":             "в 00:00, 1-го числа",
		"0 9,13,18 * * 1":       "в 09:00, 13:00, 18:00, по понедельникам",
		"0 12 13 * 5":           "в 12:00, 13-го числа или по пятницам",
		"0 0 1 1 *":             "в 00:00, 1-го числа, в январе",
		"* * * * *":             "каждую минуту, каждый день",
	}
	for expr, want := range cases {
		s, err := Parse(expr)
		if err != nil {
			t.Fatal(err)
		}
		if got := s.Describe(); got != want {
			t.Errorf("%s: %q, ожидали %q", expr, got, want)
		}
	}
}
