package ui

import (
	"testing"
	"time"
)

// Остаток считается вверх: 12 часов - это "1 дн"; истёкшим срок становится, только когда прошёл.
func TestDaysUntil(t *testing.T) {
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	cases := []struct {
		at   time.Duration
		want int
	}{
		{day / 2, 1},
		{7 * day, 7},
		{7*day + time.Hour, 8},
		{-time.Hour, 0},
		{-3*day - time.Hour, -3},
	}
	for _, c := range cases {
		if got := DaysUntil(now.Add(c.at), now); got != c.want {
			t.Errorf("%v: %d, ожидали %d", c.at, got, c.want)
		}
	}
}
