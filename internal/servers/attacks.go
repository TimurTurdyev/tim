package servers

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/TimurTurdyev/tim/internal/asn"
	"github.com/TimurTurdyev/tim/internal/baseline"
	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/reputation"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/srvinfo"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// attacksTimeout: сколько ждать скрипт журналов.
const attacksTimeout = 3 * time.Minute

// attackReport: журналы сервера, находки и владельцы сетей, откуда стучались.
type attackReport struct {
	Attacks  srvinfo.Attacks    `json:"attacks"`
	Findings []health.Check     `json:"findings"`
	Owners   []srvinfo.Owner    `json:"owners,omitempty"`
	Targeted []srvinfo.Attacker `json:"targeted,omitempty"`
	Noise    srvinfo.NoiseSum   `json:"noise"`
	Lists    bool               `json:"lists"`         // списки шума скачались
	Nets     map[string]string  `json:"nets,omitempty"` // адрес -> "AS60729 DE"
}

// collectAttacks: один заход по ssh со скриптом журналов, потом владельцы самых активных адресов.
func collectAttacks(target string, since time.Duration) (*attackReport, error) {
	// Журналы за неделю на нагруженном сервере читаются дольше минуты - предел шире, чем у Output.
	out, err := runner.OutputStdinWithin(attacksTimeout, srvinfo.AttacksScript(since), "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=15", target, "sh -s")
	if !strings.Contains(out, "@@end") {
		if err == nil {
			err = fmt.Errorf("скрипт не дошёл до конца")
		}
		return nil, err
	}
	a := srvinfo.ParseAttacks(out, time.Now(), since)
	r := &attackReport{Attacks: a, Findings: srvinfo.EvaluateAttacks(a), Nets: map[string]string{}}
	counts := srvinfo.Attackers(a)
	ips := make([]string, 0, len(counts)+len(a.Logins))
	for _, c := range counts {
		ips = append(ips, c.Key)
	}
	for _, l := range a.Logins {
		ips = append(ips, l.IP)
	}
	owners := asn.Cymru(ips)
	if len(counts) > 0 {
		r.Owners = srvinfo.GroupByOwner(counts, owners)
	}
	for ip, o := range owners {
		if o.ASN != "" {
			r.Nets[ip] = strings.TrimSpace("AS" + o.ASN + " " + o.Country)
		}
	}
	r.classify(srvinfo.ByIP(a))
	if a.SSH == srvinfo.Read {
		observeLogins(target, a.Logins)
	}
	return r, nil
}

// classify: кто в списках шума, кого спросить у GreyNoise, кто прицельный.
func (r *attackReport) classify(all []srvinfo.Attacker) {
	lists := reputation.Load()
	r.Lists = !lists.Empty()
	var wg sync.WaitGroup
	asked := 0
	for i := range all {
		all[i].Lists = lists.In(all[i].IP)
		if all[i].Candidate() && asked < 10 {
			asked++
			wg.Go(func() {
				if n, ok := reputation.GreyNoise(all[i].IP); ok {
					all[i].Noise = n.Noise
				}
			})
		}
	}
	wg.Wait()
	r.Targeted, r.Noise = srvinfo.Split(all)
	// Скрипт присылает полсотни самых активных адресов перебора; остальные - тоже шум.
	shown := 0
	for _, c := range r.Attacks.SSHByIP {
		shown += c.N
	}
	r.Noise.IPs += max(0, r.Attacks.SSHIPs-len(r.Attacks.SSHByIP))
	r.Noise.Attempts += max(0, r.Attacks.SSHFailed-shown)
}

// observeLogins: сети удачных входов - "впервые из этой сети", пока не отмечено своим.
func observeLogins(target string, logins []srvinfo.Login) {
	items := []baseline.Item{}
	for _, l := range logins {
		items = append(items, baseline.Item{Key: srvinfo.Network(l.IP), Text: l.User + " с " + l.IP})
	}
	baseline.Observe(object(target), baseline.Snapshot{"login-net": items}, time.Now())
}

// printAttacks: что прочитано, находки, владельцы сетей и удачные входы.
func printAttacks(target string, since time.Duration, asJSON bool) error {
	r, err := collectAttacks(target, since)
	if err != nil {
		return ui.Errorf("%s: %v; маршрут и вход проверит tim ssh %s", target, err, target)
	}
	if asJSON {
		return ui.JSON(r)
	}
	ui.Println(ui.Label(target) + "  " + ui.Dim("за "+window(since)+": "+sources(r.Attacks)))
	printFindings(r.Findings)
	if len(r.Findings) == 0 {
		ui.Println("  " + ui.Label("в журналах ничего тревожного"))
	}
	if len(r.Targeted) > 0 {
		ui.Println(ui.Info("прицельно, не массовые сканеры:"))
		for _, t := range r.Targeted[:min(8, len(r.Targeted))] {
			ui.Println("  " + ui.Pad(t.IP, 17) + t.Doing() + ui.Dim(netNote(r, t.IP)))
		}
		ui.Println("  " + ui.Dim("забанить: tim server "+target+" --ban <адрес>"))
	}
	if n := r.Noise; n.IPs > 0 {
		ui.Println(ui.Info("шум: ") + fmt.Sprintf("%d адресов, %d попыток", n.IPs, n.Attempts) + ui.Dim(" "+strings.Join(n.Sources, ", ")))
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
			when := "    -      "
			if !l.Time.IsZero() {
				when = l.Time.Local().Format("01-02 15:04")
			}
			ui.Println("  " + ui.Dim(when) + "  " + ui.Pad(l.User, 16) + " " + ui.Pad(l.IP, 17) + ui.Dim(methodText(l.Method)))
		}
	}
	return nil
}

// sources: какие журналы удалось прочитать - чтобы "ничего тревожного" не путали с "ничего не видно".
func sources(a srvinfo.Attacks) string {
	var parts []string
	switch {
	case a.SSH != srvinfo.Read:
		parts = append(parts, "журнал ssh не прочитан")
	case a.SSHVia == "auth.log":
		parts = append(parts, "auth.log за время файла")
	default:
		parts = append(parts, "журнал ssh")
	}
	if n := len(a.NginxFiles); n > 0 {
		parts = append(parts, fmt.Sprintf("nginx %d %s", n, ui.Plural(n, "журнал", "журнала", "журналов")))
	} else {
		parts = append(parts, "журналов nginx не видно")
	}
	if a.UFW == srvinfo.Read {
		parts = append(parts, "ufw")
	}
	return strings.Join(parts, ", ")
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
