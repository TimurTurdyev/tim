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

// Перенос строки сводки: по точке-разделителю, продолжение под значением, подпись не рвётся, цвет не теряется.
func TestWrap(t *testing.T) {
	line := "  запущено  " + Dim("lpack Laravel :8003") + " · mercurio http.server :8090, http.server :8091 · tim :8766"
	got := WrapHanging(line, 50, 12)
	want := []string{
		"  запущено  lpack Laravel :8003",
		"            mercurio http.server :8090,",
		"            http.server :8091 · tim :8766",
	}
	if len(got) != len(want) {
		t.Fatalf("строк %d: %q", len(got), got)
	}
	for i := range want {
		if StripANSI(got[i]) != want[i] {
			t.Errorf("строка %d: %q, ожидали %q", i, StripANSI(got[i]), want[i])
		}
		if Width(got[i]) > 50 {
			t.Errorf("строка %d шире 50: %d", i, Width(got[i]))
		}
	}
	if one := WrapHanging(line, 0, 12); len(one) != 1 {
		t.Errorf("без ширины перенесено: %q", one)
	}
}

// Управляющие символы и знаки смены направления письма - в пробел; обычный текст остаётся как есть, вместе с
// двойными пробелами, которыми отчёты разделяют части строки.
func TestPlain(t *testing.T) {
	cases := map[string]string{
		"nginx  1.28":                  "nginx  1.28",
		"ok \x1b[2J\x1b]0;pwn\a x":     "ok [2J ]0;pwn x",
		"c1 \u009b31m red":             "c1 31m red",
		"line\r\n\tnext\x7f":           "line next",
		"gpj.\u202egnp.exe \u2066 end": "gpj. gnp.exe end",
		"\x1b":                         "",
		"cp1251 \xcf\xf0 ok":           "cp1251 ? ok",
	}
	for in, want := range cases {
		if got := Plain(in); got != want {
			t.Errorf("%q: %q, ожидали %q", in, got, want)
		}
	}
}
