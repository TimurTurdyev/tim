package health

import (
	"testing"
	"time"

	"github.com/TimurTurdyev/tim/internal/brand"
)

func TestMachineMood(t *testing.T) {
	noon := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		m    Machine
		mood brand.Mood
		warn int
	}{
		{"спокойно", Machine{DiskPct: 60, MemFree: 80}, brand.Calm, 0},
		{"пара адресов мимо обхода", Machine{BypassOK: 725, BypassAll: 729, MemFree: 80}, brand.Worried, 1},
		{"четверть целей в VPN", Machine{BypassOK: 500, BypassAll: 729}, brand.Upset, 1},
		{"своп не показатель, память по системе", Machine{MemFree: 12}, brand.Worried, 1},
		{"диск полон", Machine{DiskPct: 96, DiskFree: 1 << 30}, brand.Upset, 1},
		{"память неизвестна", Machine{MemFree: 0}, brand.Calm, 0},
	}
	for _, c := range cases {
		if got := c.m.Mood(noon); got != c.mood {
			t.Errorf("%s: настроение %v, ждали %v", c.name, got, c.mood)
		}
		if got := len(c.m.Warnings()); got != c.warn {
			t.Errorf("%s: предупреждений %d, ждали %d: %v", c.name, got, c.warn, c.m.Warnings())
		}
	}
}
