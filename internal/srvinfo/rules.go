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

// dbPorts: что не должно отвечать из интернета. 2375 - Docker API без TLS, это root на машине.
var dbPorts = map[int]string{
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
	for _, u := range r.Failed {
		f.add("юнит", level.Warn, u, "systemd: "+u+" упал")
	}
	if r.Reboot {
		why := "нужна перезагрузка"
		for _, p := range r.RebootPkgs {
			if p == "libc6" || strings.HasPrefix(p, "linux-image") {
				why = "нужна перезагрузка: обновлены ядро или libc, до неё работает старое"
				break
			}
		}
		f.add("перезагрузка", level.Warn, "нужна", why+fmt.Sprintf("; сервер работает %d дн", int(r.Uptime.Hours()/24)))
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
	var odd []string
	for _, l := range listening {
		if !l.Public {
			continue
		}
		if name, ok := dbPorts[l.Port]; ok {
			f.add("порт", level.Bad, strconv.Itoa(l.Port)+" "+name, name+" слушает на всех адресах: из интернета до него можно достучаться")
			continue
		}
		if l.Port == 21 {
			f.add("порт", level.Warn, "21 FTP", "FTP открыт наружу: пароли идут открытым текстом, лучше SFTP")
			continue
		}
		if !usualPorts[l.Port] {
			odd = append(odd, strconv.Itoa(l.Port))
		}
	}
	if len(odd) > 0 {
		f.add("порт", level.Warn, strings.Join(odd, " "), "открыты наружу необычные порты "+strings.Join(odd, ", ")+": проверь, что это и нужно ли")
	}
}

// access: вход по ssh и защита от перебора.
func (f *findings) access(r Report) {
	if r.NoSudoSSHD {
		f.add("sshd", level.Skip, "не проверено", "настройки sshd читаются только через sudo без пароля")
	} else {
		if r.SSHD["permitrootlogin"] == "yes" {
			f.add("sshd", level.Bad, "root по паролю", "PermitRootLogin yes: вход под root, в том числе по паролю")
		}
		if r.SSHD["passwordauthentication"] == "yes" {
			f.add("sshd", level.Warn, "вход по паролю", "PasswordAuthentication yes: пароли можно перебирать")
		}
	}
	if r.Fail2ban != "active" && r.UFW != "active" {
		text := "fail2ban не запущен"
		if r.UFW == "" {
			text += ", ufw не проверен"
		}
		f.add("защита", level.Warn, text, "ни fail2ban, ни ufw не видно: перебор паролей и сканирование ничем не ограничены")
	}
}

// services: конфиг nginx и сроки сертификатов.
func (f *findings) services(r Report, now time.Time) {
	switch r.Nginx {
	case "", "ok":
	case "nosudo":
		f.add("nginx", level.Skip, "не проверено", "nginx -t читает сертификаты, без sudo его не запустить")
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
