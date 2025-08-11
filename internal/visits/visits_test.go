package visits

import (
	"testing"
	"time"
)

func TestOrder(t *testing.T) {
	now := time.Date(2025, 10, 20, 12, 0, 0, 0, time.UTC)
	names := []string{"a", "b", "c", "d"}
	m := map[string]Entry{
		"b": {Count: 100, Last: now.AddDate(0, -6, 0)},    // давно, но часто: 100 * 0.25 = 25
		"c": {Count: 3, Last: now.Add(-30 * time.Minute)}, // только что: 3 * 4 = 12
		"d": {Count: 20, Last: now.Add(-3 * time.Hour)},   // сегодня: 20 * 2 = 40
	}
	got := Order(names, m, now)
	want := []int{3, 1, 2, 0} // d, b, c, затем непосещённый a
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("порядок %v, ожидали %v", got, want)
		}
	}
}
