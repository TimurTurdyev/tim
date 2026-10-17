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
	// Находка - по имени, у портов ещё и по номеру: текст для человека не сравниваем.
	found := map[string]level.Level{}
	for _, c := range got {
		key := c.Name
		if c.Name == "порт" {
			key += ":" + strings.Fields(c.Text)[0]
		}
		found[key] = c.Level
	}
	want := map[string]level.Level{
		"перезагрузка": level.Warn,
		"порт:21":      level.Warn,
		"порт:7777":    level.Warn,
		"юнит":         level.Warn,
		"sshd":         level.Skip,
		"nginx":        level.Skip,
		"защита":       level.Warn,
	}
	for k, l := range want {
		if got, ok := found[k]; !ok || got != l {
			t.Errorf("нет находки %q уровня %v; есть: %v", k, l, found)
		}
	}
	if _, ok := found["порт:3306"]; ok {
		t.Error("MySQL на loopback - не дыра")
	}
}

func TestEvaluateBad(t *testing.T) {
	r := Report{
		Cores: 2, Load: [3]float64{6, 5, 4},
		Disks:     []Disk{{Mount: "/", Pct: 97}},
		Listening: []Listen{{Addr: "0.0.0.0", Port: 6379, Public: true}, {Addr: "10.0.0.5", Port: 5432}},
		SSHD:      map[string]string{"permitrootlogin": "yes", "passwordauthentication": "yes"},
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

func TestAccessAndHarmlessUnits(t *testing.T) {
	key := map[string]string{"passwordauthentication": "no", "kbdinteractiveauthentication": "no"}
	pass := map[string]string{"passwordauthentication": "yes"}
	// level.OK в ожидании - находки с таким именем быть не должно.
	cases := []struct {
		name string
		r    Report
		want map[string]level.Level
	}{
		{"fail2ban, вход по паролю", Report{SSHD: pass, Fail2ban: "active"}, map[string]level.Level{"защита": level.OK, "sshd": level.Warn}},
		{"CrowdSec, только ключ", Report{SSHD: key, CrowdSec: "active"}, map[string]level.Level{"защита": level.OK, "sshd": level.OK}},
		{"только ufw, вход по паролю", Report{SSHD: pass, UFW: "active"}, map[string]level.Level{"защита": level.Warn}},
		{"sshd не проверен", Report{NoSudoSSHD: true}, map[string]level.Level{"защита": level.Warn}},
		{"root по ключу, без защиты", Report{SSHD: map[string]string{"passwordauthentication": "no", "permitrootlogin": "yes"}},
			map[string]level.Level{"защита": level.Info, "sshd": level.OK}},
		{"fwupd на виртуалке, ifup при netplan", Report{SSHD: key, Fail2ban: "active", Virt: "kvm", Netplan: true,
			Failed: []string{"fwupd.service", "ifup@eth0.service"}}, map[string]level.Level{"юнит": level.Info}},
		{"fwupd на железе, networking без netplan", Report{SSHD: key, Fail2ban: "active", Virt: "none",
			Failed: []string{"fwupd.service", "networking.service"}}, map[string]level.Level{"юнит": level.Warn}},
	}
	for _, c := range cases {
		got := map[string]level.Level{}
		for _, x := range Evaluate(c.r, time.Now()) {
			got[x.Name] = max(got[x.Name], x.Level)
		}
		for name, want := range c.want {
			l, ok := got[name]
			if want == level.OK && ok || want != level.OK && l != want {
				t.Errorf("%s: %s = %v, ждали %v (есть: %v)", c.name, name, l, want, got)
			}
		}
	}
}
