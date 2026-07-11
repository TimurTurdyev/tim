package srvinfo

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/TimurTurdyev/tim/internal/level"
)

func TestParseUbuntu(t *testing.T) {
	data, err := os.ReadFile("testdata/ubuntu.txt")
	if err != nil {
		t.Fatal(err)
	}
	r := Parse(string(data))
	if r.OS != "Ubuntu 24.04.3 LTS" || r.Cores != 2 || r.Uptime.Hours()/24 < 172 || r.Load[0] != 0.08 {
		t.Fatalf("%+v", r)
	}
	if len(r.Disks) != 1 || r.Disks[0].Mount != "/" || r.Disks[0].Pct != 50 || r.Inodes[0].Pct != 8 {
		t.Fatalf("диски %+v %+v", r.Disks, r.Inodes)
	}
	if !r.Reboot || len(r.Failed) != 2 || !r.NoSudoSSHD || r.Nginx != "nosudo" || r.Fail2ban != "inactive" {
		t.Fatalf("%+v", r)
	}
	// Пакеты в reboot-required повторяются; ядро и libc должны остаться по одному разу.
	if strings.Count(strings.Join(r.RebootPkgs, " "), "libc6") != 1 {
		t.Fatalf("пакеты: %v", r.RebootPkgs)
	}
	ports := map[int]bool{}
	for _, l := range r.Listening {
		ports[l.Port] = l.Public
	}
	// 3306 на loopback, 80 на публичном адресе, 22 на всех: v4 и v6 одной строкой.
	if ports[3306] || !ports[80] || !ports[22] || len(r.Listening) != 16 {
		t.Fatalf("порты: %+v", r.Listening)
	}
}

func TestEvaluateUbuntu(t *testing.T) {
	data, _ := os.ReadFile("testdata/ubuntu.txt")
	got := Evaluate(Parse(string(data)), time.Now())
	text := map[string]level.Level{}
	for _, c := range got {
		text[c.Name+":"+c.Text] = c.Level
	}
	want := map[string]level.Level{
		"перезагрузка:нужна":      level.Warn,
		"порт:21 FTP":             level.Warn,
		"порт:7777 8888":          level.Warn,
		"юнит:networking.service": level.Warn,
		"sshd:не проверено":       level.Skip,
		"nginx:не проверено":      level.Skip,
		"защита:fail2ban не запущен, ufw не проверен": level.Warn,
	}
	for k, l := range want {
		if got, ok := text[k]; !ok || got != l {
			t.Errorf("нет находки %q уровня %v; есть: %v", k, l, text)
		}
	}
	for k := range text {
		if strings.HasPrefix(k, "порт:3306") {
			t.Error("MySQL на loopback - не дыра")
		}
	}
}

func TestEvaluateBad(t *testing.T) {
	r := Report{
		Cores: 2, Load: [3]float64{6, 5, 4},
		Disks:     []Disk{{Mount: "/", Pct: 97}},
		Listening: []Listen{{Addr: "0.0.0.0", Port: 6379, Public: true}, {Addr: "10.0.0.5", Port: 5432}},
		SSHD:      map[string]string{"permitrootlogin": "yes", "passwordauthentication": "no"},
		Fail2ban:  "active",
		Nginx:     `nginx: [emerg] unknown directive "servr_name"`,
	}
	got := Evaluate(r, time.Now())
	bad := 0
	for _, c := range got {
		if c.Level == level.Bad {
			bad++
		}
		if strings.Contains(c.Text, "5432") {
			t.Error("PostgreSQL на частном адресе - не наружу")
		}
	}
	// диск 97%, нагрузка 2.5 на ядро, Redis наружу, root по паролю, nginx с ошибкой
	if bad != 5 || got[0].Level != level.Bad {
		t.Fatalf("плохих %d: %+v", bad, got)
	}
}
