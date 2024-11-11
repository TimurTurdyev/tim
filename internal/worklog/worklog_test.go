package worklog

import (
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	out := "aaa\x1f2025-10-20T10:00:00+03:00\x1ffix(cart): не терять строки\n" +
		"битая строка\n" +
		"bbb\x1fне дата\x1fтема\n" +
		"ccc\x1f2025-10-20T12:30:00+03:00\x1f тема с пробелами \n"
	got := Parse(out)
	if len(got) != 2 || got[0].Hash != "aaa" || got[1].Subject != "тема с пробелами" {
		t.Fatalf("%+v", got)
	}
}

func TestGroupDedupAndOrder(t *testing.T) {
	at := func(h int) time.Time { return time.Date(2025, 10, 20, h, 0, 0, 0, time.UTC) }
	by := map[string][]Commit{
		"site":     {{Hash: "1", When: at(9)}, {Hash: "2", When: at(11)}},
		"handbook": {{Hash: "2", When: at(11)}}, // вложенная папка того же репозитория
		"api":      {{Hash: "3", When: at(10)}, {Hash: "4", When: at(12)}, {Hash: "5", When: at(8)}},
	}
	got := Group(by, []string{"site", "handbook", "api"})
	if len(got) != 2 || got[0].Name != "api" || got[1].Name != "site" {
		t.Fatalf("порядок или дубли: %+v", got)
	}
	if got[0].Commits[0].Hash != "4" {
		t.Fatalf("свежий коммит должен быть первым: %+v", got[0].Commits)
	}
}

func TestSince(t *testing.T) {
	wed := time.Date(2025, 10, 22, 15, 4, 0, 0, time.UTC)
	if s := Since(wed, false); !s.Equal(time.Date(2025, 10, 22, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("сегодня: %v", s)
	}
	if s := Since(wed, true); !s.Equal(time.Date(2025, 10, 20, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("неделя с понедельника: %v", s)
	}
	sun := time.Date(2025, 10, 26, 9, 0, 0, 0, time.UTC)
	if s := Since(sun, true); !s.Equal(time.Date(2025, 10, 20, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("воскресенье относится к неделе с понедельника: %v", s)
	}
}
