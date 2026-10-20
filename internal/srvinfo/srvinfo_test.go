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
	if !r.Reboot || len(r.Failed) != 2 || !r.NoSudoSSHD || r.Nginx != "nosudo" || r.Fail2ban {
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
		Fail2ban:  true,
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
		{"fail2ban, вход по паролю", Report{SSHD: pass, Fail2ban: true}, map[string]level.Level{"защита": level.OK, "sshd": level.Warn}},
		{"CrowdSec, только ключ", Report{SSHD: key, CrowdSec: true}, map[string]level.Level{"защита": level.OK, "sshd": level.OK}},
		{"только ufw, вход по паролю", Report{SSHD: pass, UFW: "active"}, map[string]level.Level{"защита": level.Warn}},
		{"sshd не проверен", Report{NoSudoSSHD: true}, map[string]level.Level{"защита": level.Warn}},
		{"root по ключу, без защиты", Report{SSHD: map[string]string{"passwordauthentication": "no", "permitrootlogin": "yes"}},
			map[string]level.Level{"защита": level.Info, "sshd": level.OK}},
		{"fwupd на виртуалке, ifup при netplan", Report{SSHD: key, Fail2ban: true, Virt: "kvm", Netplan: true,
			Failed: []string{"fwupd.service", "ifup@eth0.service"}}, map[string]level.Level{"юнит": level.Info}},
		{"fwupd на железе, networking без netplan", Report{SSHD: key, Fail2ban: true, Virt: "none",
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

func TestAttacks(t *testing.T) {
	data, err := os.ReadFile("testdata/attacks.txt")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.FixedZone("MSK", 3*3600))
	a := ParseAttacks(string(data), now, 24*time.Hour)
	// Повтор во frontend и backend - одна проба; свой tim audit и проба старше суток не считаются.
	if a.SSHFailed != 447 || a.SSHIPs != 2 || len(a.Logins) != 2 || len(a.Probes) != 2 || a.Probes[1].Path != "/wp-login.php" ||
		a.UFWBlocked != 120 || len(a.Jails) != 1 || a.Jails[0].Banned != 3 || a.Password != PasswordOn {
		t.Fatalf("%+v", a)
	}
	got := map[string]level.Level{}
	for _, c := range EvaluateAttacks(a) {
		got[c.Name] = max(got[c.Name], c.Level)
	}
	// admin вошёл по паролю с адреса, который перебирал пароли, - плохо; перебор без защиты - внимание;
	// 200 на wp-login - перебор входа, к сведению, а не открытый файл.
	if got["вход"] != level.Bad || got["ssh"] != level.Warn || got["сайт"] != level.Info {
		t.Fatalf("%v", got)
	}
}
