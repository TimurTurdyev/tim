package srvinfo

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/level"
)

// DBPorts: что не должно отвечать из интернета. 2375 - Docker API без TLS, это root на машине.
var DBPorts = map[int]string{
	3306: "MySQL", 5432: "PostgreSQL", 6379: "Redis", 27017: "MongoDB", 9200: "Elasticsearch",
	11211: "Memcached", 5984: "CouchDB", 8086: "InfluxDB", 2375: "Docker API без TLS", 2376: "Docker API",
}

// usualPorts: открытые наружу порты, которые на сервере сайтов и почты ожидаемы.
var usualPorts = map[int]bool{22: true, 80: true, 443: true, 25: true, 465: true, 587: true,
	110: true, 143: true, 993: true, 995: true, 53: true}

func byPercent(pct, warn, bad int) level.Level {
	switch {
	case pct >= bad:
		return level.Bad
	case pct >= warn:
		return level.Warn
	}
	return level.OK
}

// Evaluate превращает отчёт в находки, худшие первыми.
func Evaluate(r Report, now time.Time) []health.Check {
	var f findings
	f.resources(r)
	f.system(r)
	f.ports(r.Listening)
	f.access(r)
	f.services(r, now)
	slices.SortStableFunc(f, func(a, b health.Check) int { return cmp.Compare(b.Level, a.Level) })
	return f
}

// findings: находки по серверу в порядке правил.
type findings []health.Check

func (f *findings) add(name string, l level.Level, text, detail string) {
	*f = append(*f, health.Check{Name: name, Level: l, Text: text, Detail: detail})
}

// resources: диски, inode, память, своп и нагрузка.
func (f *findings) resources(r Report) {
	for _, d := range r.Disks {
		if l := byPercent(d.Pct, 85, 95); l > level.OK {
			f.add("диск", l, d.Mount+" "+strconv.Itoa(d.Pct)+"%", fmt.Sprintf("на %s занято %d%%", d.Mount, d.Pct))
		}
	}
	for _, d := range r.Inodes {
		if l := byPercent(d.Pct, 85, 95); l > level.OK {
			f.add("inode", l, d.Mount+" "+strconv.Itoa(d.Pct)+"%", fmt.Sprintf("на %s кончаются inode (%d%%): место есть, а файл не создать", d.Mount, d.Pct))
		}
	}
	if r.MemTotal > 0 && r.MemAvail*10 < r.MemTotal {
		f.add("память", level.Warn, "свободно меньше 10%", "доступной памяти меньше 10%: скоро придёт OOM killer")
	}
	if r.SwapTotal > 0 && (r.SwapTotal-r.SwapFree)*2 > r.SwapTotal {
		f.add("swap", level.Warn, "занят больше половины", "своп занят больше чем наполовину - памяти не хватает постоянно")
	}
	if r.Cores > 0 {
		per := r.Load[1] / float64(r.Cores)
		switch {
		case per > 2:
			f.add("нагрузка", level.Bad, fmt.Sprintf("%.1f на ядро", per), "средняя нагрузка за 5 минут вдвое выше числа ядер")
		case per > 1:
			f.add("нагрузка", level.Warn, fmt.Sprintf("%.1f на ядро", per), "средняя нагрузка за 5 минут выше числа ядер")
		}
	}
}

// system: упавшие юниты, ожидание перезагрузки и обновления безопасности.
func (f *findings) system(r Report) {
	var harmless, reasons []string
	for _, u := range r.Failed {
		why := Harmless(u, r)
		if why == "" {
			f.add("юнит", level.Warn, u, "systemd: "+u+" упал")
			continue
		}
		harmless = append(harmless, u)
		if !slices.Contains(reasons, why) {
			reasons = append(reasons, why)
		}
	}
	if len(harmless) > 0 {
		f.add("юнит", level.Info, strings.Join(harmless, " "),
			"упали без последствий: "+strings.Join(harmless, ", ")+" - "+strings.Join(reasons, "; "))
	}
	if r.Reboot {
		why := "после обновлений"
		for _, p := range r.RebootPkgs {
			if p == "libc6" || strings.HasPrefix(p, "linux-image") {
				why = "обновлены ядро или libc, до перезагрузки работает старое"
				break
			}
		}
		f.add("перезагрузка", level.Warn, "нужна", "нужна: "+why+fmt.Sprintf("; сервер работает %d дн", int(r.Uptime.Hours()/24)))
	}
	if n := len(r.SecurityUpdates); n > 0 {
		list := strings.Join(r.SecurityUpdates[:min(5, n)], ", ")
		if n > 5 {
			list += " и другие"
		}
		f.add("обновления", level.Warn, strconv.Itoa(n)+" безопасности", "ждут обновления безопасности: "+list)
	}
}

// ports: базы и FTP, открытые наружу, и необычные порты одной строкой.
func (f *findings) ports(listening []Listen) {
	var odd []int
	for _, l := range listening {
		if !l.Public {
			continue
		}
		if name, ok := DBPorts[l.Port]; ok {
			f.add("порт", level.Bad, strconv.Itoa(l.Port)+" "+name, name+" слушает на всех адресах: из интернета до него можно достучаться")
			continue
		}
		if l.Port == 21 {
			f.add("порт", level.Warn, "21 FTP", "FTP открыт наружу: пароли идут открытым текстом, лучше SFTP")
			continue
		}
		if !usualPorts[l.Port] {
			odd = append(odd, l.Port)
		}
	}
	if len(odd) > 0 {
		slices.Sort(odd)
		list := make([]string, len(odd))
		for i, p := range odd {
			list[i] = strconv.Itoa(p)
		}
		f.add("порт", level.Warn, strings.Join(list, " "), "открыты наружу необычные порты "+strings.Join(list, ", ")+": проверь, что это и нужно ли")
	}
}

// access: вход по ssh и защита от перебора.
// ufw защитой от перебора не считается: он закрывает порты, а открытый 22 не бережёт.
func (f *findings) access(r Report) {
	password := PasswordLogin(r)
	if r.NoSudoSSHD {
		f.add("sshd", level.Skip, "не проверено", "не проверен: настройки читаются только через sudo без пароля")
	} else {
		if r.SSHD["permitrootlogin"] == "yes" && password {
			f.add("sshd", level.Bad, "root по паролю", "PermitRootLogin yes и вход по паролю: пароль root можно подбирать")
		}
		if password {
			f.add("sshd", level.Warn, "вход по паролю", "вход по паролю включён: пароли можно перебирать")
		}
	}
	switch {
	case r.Fail2ban == "active" || r.CrowdSec == "active":
	case password || r.NoSudoSSHD:
		f.add("защита", level.Warn, "нет fail2ban и CrowdSec", "перебор паролей по ssh ничем не ограничен: нет ни fail2ban, ни CrowdSec")
	default:
		f.add("защита", level.Info, "вход только по ключу", "нет ни fail2ban, ни CrowdSec, но вход только по ключу: перебор не опасен, fail2ban уберёт шум из журнала")
	}
}

// PasswordLogin: sshd пускает по паролю - напрямую или через keyboard-interactive.
func PasswordLogin(r Report) bool {
	return r.SSHD["passwordauthentication"] == "yes" || r.SSHD["kbdinteractiveauthentication"] == "yes"
}

// Harmless: почему падение юнита ни на что не влияет, пусто - влияет. На VPS это частый шум: fwupd
// обновляет прошивки, которых у виртуальной машины нет; ifupdown падает, когда сетью управляет netplan.
func Harmless(unit string, r Report) string {
	switch {
	case strings.HasPrefix(unit, "fwupd") && r.Virt != "" && r.Virt != "none":
		return "на виртуальной машине прошивок нет"
	case (unit == "networking.service" || strings.HasPrefix(unit, "ifup@")) && r.Netplan:
		return "сетью управляет netplan, старый ifupdown не нужен"
	}
	return ""
}

// services: конфиг nginx и сроки сертификатов.
func (f *findings) services(r Report, now time.Time) {
	switch r.Nginx {
	case "", "ok":
	case "nosudo":
		f.add("nginx", level.Skip, "не проверено", "не проверен: nginx -t читает сертификаты, ему нужен sudo без пароля")
	default:
		f.add("nginx", level.Bad, "конфиг с ошибкой", "nginx -t: "+r.Nginx+" - следующий reload не пройдёт")
	}
	for _, c := range r.Certs {
		if d := health.DeadlineAt("сертификат "+c.Name, c.Expires, now, health.CertWarnDays, health.CertBadDays); d.Level > level.OK {
			d.Name = "сертификат"
			*f = append(*f, d)
		}
	}
}
