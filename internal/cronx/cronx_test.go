package cronx

import (
	"testing"
	"time"
)

func TestDescribe(t *testing.T) {
	cases := map[string]string{
		"* * * * *":       "каждую минуту, каждый день",
		"*/15 * * * *":    "каждые 15 мин, каждый день",
		"30 3 * * *":      "в 03:30, каждый день",
		"@daily":          "в 00:00, каждый день",
		"5 * * * *":       "в 5-ю минуту каждого часа, каждый день",
		"0 9 * * mon-fri": "в 09:00, по дням: понедельник, вторник, среда, четверг, пятница",
		"0 0 1 jan,jul *": "в 00:00, числа 1, месяц январь, июль",
		"0 12 13 * 5":     "в 12:00, числа 13 или по дням: пятница",
		"0 8-18/2 * * *":  "в 00 минут, часы 8, 10, 12, 14, 16, 18, каждый день",
		"0 0 * * 7":       "в 00:00, по дням: воскресенье",
	}
	for expr, want := range cases {
		s, err := Parse(expr)
		if err != nil {
			t.Errorf("%s: %v", expr, err)
			continue
		}
		if got := s.Describe(); got != want {
			t.Errorf("%s:\n got  %s\n want %s", expr, got, want)
		}
	}
	for _, bad := range []string{"* * * *", "60 * * * *", "* 24 * * *", "*/0 * * * *", "5-1 * * * *", "x * * * *"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
}

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
