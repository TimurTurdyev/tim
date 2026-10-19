package servers

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/asn"
	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/srvinfo"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// attackReport: журналы сервера, находки и владельцы сетей, откуда стучались.
type attackReport struct {
	Attacks  srvinfo.Attacks `json:"attacks"`
	Findings []health.Check  `json:"findings"`
	Owners   []srvinfo.Owner `json:"owners,omitempty"`
}

// collectAttacks: один заход по ssh со скриптом журналов, потом владельцы самых активных адресов.
func collectAttacks(target string, since time.Duration) (*attackReport, error) {
	out, err := runner.OutputStdin(srvinfo.AttacksScript(since), "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=15", target, "sh -s")
	if !strings.Contains(out, "@@end") {
		if err == nil {
			err = fmt.Errorf("скрипт не дошёл до конца")
		}
		return nil, err
	}
	a := srvinfo.ParseAttacks(out, time.Now(), since)
	r := &attackReport{Attacks: a, Findings: srvinfo.EvaluateAttacks(a)}
	counts := attackers(a)
	if len(counts) > 0 {
		ips := make([]string, len(counts))
		for i, c := range counts {
			ips[i] = c.Key
		}
		r.Owners = srvinfo.GroupByOwner(counts, asn.Cymru(ips))
	}
	return r, nil
}

// printAttacks: что прочитано, находки, владельцы сетей и удачные входы.
func printAttacks(target string, since time.Duration, asJSON bool) error {
	r, err := collectAttacks(target, since)
	if err != nil {
		return ui.Errorf("%s: %v; маршрут и вход проверит tim ssh %s", target, err, target)
	}
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	}
	ui.Println(ui.Label(target) + "  " + ui.Dim("за "+window(since)+": "+sources(r.Attacks)))
	for _, c := range r.Findings {
		ui.Println("  " + level.Lamp(c.Level, ui.Pad(c.Name, 10)+c.Detail))
	}
	if len(r.Findings) == 0 {
		ui.Println("  " + ui.Label("в журналах ничего тревожного"))
	}
	if len(r.Owners) > 0 {
		ui.Println(ui.Info("откуда больше всего:"))
		for _, o := range r.Owners[:min(5, len(r.Owners))] {
			ui.Println("  " + ui.Pad(ui.Clip(ownerName(o), 48), 50) + ui.Dim(attemptsText(o)))
		}
	}
	if len(r.Attacks.Logins) > 0 {
		ui.Println(ui.Info("удачные входы:"))
		for _, l := range r.Attacks.Logins[max(0, len(r.Attacks.Logins)-10):] {
			ui.Println("  " + ui.Dim(l.Time.Local().Format("01-02 15:04")) + "  " + ui.Pad(l.User, 12) + ui.Pad(l.IP, 18) + ui.Dim(methodText(l.Method)))
		}
	}
	return nil
}

// attackers: адреса со всех источников и сколько с каждого пришло, самые активные первыми.
func attackers(a srvinfo.Attacks) []srvinfo.Count {
	byIP := map[string]int{}
	for _, c := range a.SSHByIP {
		byIP[c.Key] += c.N
	}
	for _, c := range a.UFWByIP {
		byIP[c.Key] += c.N
	}
	for _, p := range a.Probes {
		byIP[p.IP]++
	}
	var out []srvinfo.Count
	for ip, n := range byIP {
		out = append(out, srvinfo.Count{Key: ip, N: n})
	}
	slices.SortFunc(out, func(x, y srvinfo.Count) int { return cmp.Or(cmp.Compare(y.N, x.N), cmp.Compare(x.Key, y.Key)) })
	return out[:min(50, len(out))]
}

// sources: какие журналы удалось прочитать - чтобы "ничего тревожного" не путали с "ничего не видно".
func sources(a srvinfo.Attacks) string {
	var parts []string
	switch a.SSHSource {
	case "none", "":
		parts = append(parts, "журнал ssh не прочитан")
	default:
		parts = append(parts, "журнал ssh")
	}
	if n := len(a.NginxFiles); n > 0 {
		parts = append(parts, fmt.Sprintf("nginx %d %s", n, ui.Plural(n, "журнал", "журнала", "журналов")))
	} else {
		parts = append(parts, "журналов nginx не видно")
	}
	if a.UFWState != "none" {
		parts = append(parts, "ufw")
	}
	return strings.Join(parts, ", ")
}

// parseSince: окно отчёта - 24h, 3d, 1w; дни и недели time.ParseDuration не знает.
func parseSince(s string) (time.Duration, error) {
	for suffix, unit := range map[string]time.Duration{"d": 24 * time.Hour, "w": 7 * 24 * time.Hour} {
		if n, ok := strings.CutSuffix(s, suffix); ok {
			v, err := strconv.Atoi(n)
			if err != nil || v <= 0 {
				return 0, ui.Errorf("окно %s: нужно вроде 24h, 3d или 1w", s)
			}
			return time.Duration(v) * unit, nil
		}
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, ui.Errorf("окно %s: нужно вроде 24h, 3d или 1w", s)
	}
	return d, nil
}

// window: окно отчёта словами - 24 ч, 3 дн.
func window(d time.Duration) string {
	if d%(24*time.Hour) == 0 && d >= 48*time.Hour {
		return fmt.Sprintf("%d дн", d/(24*time.Hour))
	}
	return fmt.Sprintf("%d ч", int(d.Hours()))
}

func ownerName(o srvinfo.Owner) string {
	if o.ASN == "" {
		return o.Name
	}
	name := "AS" + o.ASN + " " + o.Name
	if o.Country != "" {
		name += " (" + o.Country + ")"
	}
	return name
}

func attemptsText(o srvinfo.Owner) string {
	return fmt.Sprintf("%d %s с %d %s", o.Attempts, ui.Plural(o.Attempts, "попытка", "попытки", "попыток"),
		o.IPs, ui.Plural(o.IPs, "адреса", "адресов", "адресов"))
}

func methodText(m string) string {
	switch m {
	case "publickey":
		return "по ключу"
	case "password":
		return "по паролю"
	}
	return m
}
