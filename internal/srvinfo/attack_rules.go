package srvinfo

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/TimurTurdyev/tim/internal/asn"
	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Подсказки к находкам по журналам.
const (
	fixReadAccess = "журналы читают root и группы adm и systemd-journal: sudo usermod -aG adm,systemd-journal <пользователь>"
	fixBanLogin   = "проверь, твой ли это вход: last -i; если нет - смени пароль, выключи вход по паролю и поставь fail2ban"
	fixKeyLogin   = "вход по ключу надёжнее: ssh-copy-id, затем PasswordAuthentication no"
	fixCMSLogin   = "форму входа CMS закрывают fail2ban (фильтр wordpress) или ограничение по адресам в nginx"
)

// loginPaths: входы в CMS - на них 200 законно, зато их перебирают.
var loginPaths = []string{"/wp-login.php", "/xmlrpc.php"}

// Owner: владелец сети и сколько с его адресов пришло.
type Owner struct {
	ASN      string `json:"asn"`
	Name     string `json:"name"`
	Country  string `json:"country"`
	Attempts int    `json:"attempts"`
	IPs      int    `json:"ips"`
}

// EvaluateAttacks: находки по журналам. Сканирование само по себе - шум; важны удачные входы, особенно
// после перебора, пробы сайта, на которые ответили 200, и есть ли защита от перебора.
func EvaluateAttacks(a Attacks) []health.Check {
	var f findings
	f.logins(a)
	f.bruteforce(a)
	f.probes(a.Probes)
	if a.UFWBlocked > 0 {
		detail := "ufw отбил " + attempts(a.UFWBlocked)
		if len(a.UFWByPort) > 0 {
			detail += ", чаще всего порты " + keys(a.UFWByPort, 5)
		}
		f.add("файрвол", level.Info, fmt.Sprintf("%d отбито", a.UFWBlocked), detail, "")
	}
	f.bans(a)
	slices.SortStableFunc(f, func(x, y health.Check) int { return cmp.Compare(y.Level, x.Level) })
	return f
}

// Attackers: адреса со всех источников и сколько с каждого пришло, самые активные первыми, не больше 50.
func Attackers(a Attacks) []Count {
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
	var out []Count
	for ip, n := range byIP {
		out = append(out, Count{Key: ip, N: n})
	}
	slices.SortFunc(out, func(x, y Count) int { return cmp.Or(cmp.Compare(y.N, x.N), cmp.Compare(x.Key, y.Key)) })
	return out[:min(50, len(out))]
}

// GroupByOwner: попытки по владельцам сетей, самые активные сверху; адрес без владельца - сам по себе.
func GroupByOwner(counts []Count, owners map[string]asn.ASInfo) []Owner {
	byKey := map[string]*Owner{}
	var out []*Owner
	for _, c := range counts {
		info, ok := owners[c.Key]
		key := c.Key
		o := Owner{Name: c.Key}
		if ok && info.ASN != "" {
			key, o = "AS"+info.ASN, Owner{ASN: info.ASN, Name: info.Name, Country: info.Country}
		}
		if byKey[key] == nil {
			byKey[key] = &o
			out = append(out, &o)
		}
		byKey[key].Attempts += c.N
		byKey[key].IPs++
	}
	slices.SortStableFunc(out, func(x, y *Owner) int { return cmp.Compare(y.Attempts, x.Attempts) })
	res := make([]Owner, len(out))
	for i, o := range out {
		res[i] = *o
	}
	return res
}

func (f *findings) logins(a Attacks) {
	failed := map[string]int{}
	for _, c := range a.SSHByIP {
		failed[c.Key] = c.N
	}
	seen := map[string]bool{}
	for _, l := range a.Logins {
		key := l.User + " " + l.IP + " " + l.Method
		if seen[key] {
			continue
		}
		seen[key] = true
		switch n := failed[l.IP]; {
		case n > 0:
			f.add("вход", level.Bad, l.User+" с "+l.IP,
				fmt.Sprintf("вход %s с адреса, который перебирал пароли: %d %s до этого", l.User, n, ui.Plural(n, "неудача", "неудачи", "неудач")), fixBanLogin)
		case l.Method != "publickey":
			f.add("вход", level.Warn, l.User+" по паролю с "+l.IP, "вход по паролю: "+l.User+" с "+l.IP, fixKeyLogin)
		}
	}
}

func (f *findings) bruteforce(a Attacks) {
	if a.SSH != Read {
		f.add("ssh", level.Skip, "журнал не прочитан", "журнал ssh не прочитан: "+fixReadAccess, fixReadAccess)
		return
	}
	if a.SSHFailed == 0 {
		return
	}
	what := "перебор ssh: " + attempts(a.SSHFailed) + " с " + addrs(a.SSHIPs)
	if len(a.SSHUsers) > 0 {
		what += ", чаще всего " + keys(a.SSHUsers, 3)
	}
	switch {
	case a.Fail2ban || a.CrowdSec:
		f.add("ssh", level.Info, "перебор, защита работает", what, "")
	case a.Password == PasswordOff:
		f.add("ssh", level.Info, "перебор, вход только по ключу", what+"; вход только по ключу - пароль не подобрать", "")
	default:
		f.add("ssh", level.Warn, "перебор без защиты", what+"; ни fail2ban, ни CrowdSec не работают", fixProtect)
	}
}

// probes: пробы, на которые ответили 200, - по пути; перебор входа в CMS - отдельно, на входе 200 законно;
// остальные одной строкой.
func (f *findings) probes(list []Probe) {
	open := map[string]int{}
	var paths []string
	ips := map[string]bool{}
	logins, blocked := 0, 0
	for _, p := range list {
		ips[p.IP] = true
		switch {
		case slices.Contains(loginPaths, p.Path):
			logins++
		case p.Status == 200:
			if open[p.Path] == 0 {
				paths = append(paths, p.Path)
			}
			open[p.Path]++
		default:
			blocked++
		}
	}
	for _, p := range paths {
		f.add("сайт", level.Warn, "200 на "+p, fmt.Sprintf("на %s ответили 200 (%d раз): проверь, что там не файл, а заглушка", p, open[p]),
			"tim audit <домен> проверит, файл это или заглушка")
	}
	if logins > 0 {
		f.add("сайт", level.Info, fmt.Sprintf("%d к форме входа", logins), fmt.Sprintf("перебор входа в CMS: %d %s к wp-login и xmlrpc", logins, ui.Plural(logins, "запрос", "запроса", "запросов")), fixCMSLogin)
	}
	if blocked > 0 {
		f.add("сайт", level.Info, fmt.Sprintf("%d отбито", blocked),
			fmt.Sprintf("пробы .env, .git и панелей: %d %s с %s, отбиты", blocked, ui.Plural(blocked, "запрос", "запроса", "запросов"), addrs(len(ips))), "")
	}
}

func (f *findings) bans(a Attacks) {
	switch a.F2B {
	case NoSudo:
		f.add("fail2ban", level.Skip, "не проверено", "fail2ban-client отвечает только root: кого он банит, не видно", "смотреть под root: sudo fail2ban-client status sshd")
	case Read:
		now, total := 0, 0
		for _, j := range a.Jails {
			now, total = now+j.Banned, total+j.Total
		}
		if len(a.Jails) > 0 {
			f.add("fail2ban", level.Info, fmt.Sprintf("забанено %d", now), fmt.Sprintf("fail2ban: сейчас в бане %s, всего за работу %d", banned(now), total), "")
		}
	}
	if a.CS == Read && len(a.Decisions) > 0 {
		f.add("CrowdSec", level.Info, fmt.Sprintf("забанено %d", len(a.Decisions)), "CrowdSec: сейчас в бане "+banned(len(a.Decisions)), "")
	}
}

func attempts(n int) string {
	return fmt.Sprintf("%d %s", n, ui.Plural(n, "попытку", "попытки", "попыток"))
}

func addrs(n int) string {
	return fmt.Sprintf("%d %s", n, ui.Plural(n, "адреса", "адресов", "адресов"))
}

func banned(n int) string {
	return fmt.Sprintf("%d %s", n, ui.Plural(n, "адрес", "адреса", "адресов"))
}

func keys(list []Count, n int) string {
	var out []string
	for _, c := range list[:min(n, len(list))] {
		out = append(out, c.Key)
	}
	return strings.Join(out, ", ")
}
