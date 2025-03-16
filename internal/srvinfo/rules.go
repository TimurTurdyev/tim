package srvinfo

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/health"
)

// dbPorts: что не должно отвечать из интернета. 2375 - Docker API без TLS, это root на машине.
var dbPorts = map[int]string{
	3306: "MySQL", 5432: "PostgreSQL", 6379: "Redis", 27017: "MongoDB", 9200: "Elasticsearch",
	11211: "Memcached", 5984: "CouchDB", 8086: "InfluxDB", 2375: "Docker API без TLS", 2376: "Docker API",
}

// usualPorts: открытые наружу порты, которые на сервере сайтов и почты ожидаемы.
var usualPorts = map[int]bool{22: true, 80: true, 443: true, 25: true, 465: true, 587: true,
	110: true, 143: true, 993: true, 995: true, 53: true}

func level(pct, warn, bad int) health.Level {
	switch {
	case pct >= bad:
		return health.Bad
	case pct >= warn:
		return health.Warn
	}
	return health.OK
}

// Evaluate превращает отчёт в находки, худшие первыми.
func Evaluate(r Report, now time.Time) []health.Check {
	var out []health.Check
	add := func(name string, l health.Level, text, detail string) {
		out = append(out, health.Check{Name: name, Level: l, Text: text, Detail: detail})
	}
	for _, d := range r.Disks {
		if l := level(d.Pct, 85, 95); l > health.OK {
			add("диск", l, d.Mount+" "+strconv.Itoa(d.Pct)+"%", fmt.Sprintf("на %s занято %d%%", d.Mount, d.Pct))
		}
	}
	for _, d := range r.Inodes {
		if l := level(d.Pct, 85, 95); l > health.OK {
			add("inode", l, d.Mount+" "+strconv.Itoa(d.Pct)+"%", fmt.Sprintf("на %s кончаются inode (%d%%): место есть, а файл не создать", d.Mount, d.Pct))
		}
	}
	if r.MemTotal > 0 && r.MemAvail*10 < r.MemTotal {
		add("память", health.Warn, "свободно меньше 10%", "доступной памяти меньше 10%: скоро придёт OOM killer")
	}
	if r.SwapTotal > 0 && (r.SwapTotal-r.SwapFree)*2 > r.SwapTotal {
		add("swap", health.Warn, "занят больше половины", "своп занят больше чем наполовину - памяти не хватает постоянно")
	}
	if r.Cores > 0 {
		per := r.Load[1] / float64(r.Cores)
		switch {
		case per > 2:
			add("нагрузка", health.Bad, fmt.Sprintf("%.1f на ядро", per), "средняя нагрузка за 5 минут вдвое выше числа ядер")
		case per > 1:
			add("нагрузка", health.Warn, fmt.Sprintf("%.1f на ядро", per), "средняя нагрузка за 5 минут выше числа ядер")
		}
	}
	for _, u := range r.Failed {
		add("юнит", health.Warn, u, "systemd: "+u+" упал")
	}
	if r.Reboot {
		why := "нужна перезагрузка"
		for _, p := range r.RebootPkgs {
			if p == "libc6" || strings.HasPrefix(p, "linux-image") {
				why = "нужна перезагрузка: обновлены ядро или libc, до неё работает старое"
				break
			}
		}
		add("перезагрузка", health.Warn, "нужна", why+fmt.Sprintf("; сервер работает %d дн", int(r.Uptime.Hours()/24)))
	}
	if n := len(r.SecurityUpdates); n > 0 {
		list := r.SecurityUpdates[:min(5, n)]
		add("обновления", health.Warn, strconv.Itoa(n)+" безопасности",
			"ждут обновления безопасности: "+strings.Join(list, ", ")+map[bool]string{true: " и другие", false: ""}[n > 5])
	}
	var odd []string
	for _, l := range r.Listening {
		if !l.Public {
			continue
		}
		if name, ok := dbPorts[l.Port]; ok {
			add("порт", health.Bad, strconv.Itoa(l.Port)+" "+name, name+" слушает на всех адресах: из интернета до него можно достучаться")
			continue
		}
		if l.Port == 21 {
			add("порт", health.Warn, "21 FTP", "FTP открыт наружу: пароли идут открытым текстом, лучше SFTP")
			continue
		}
		if !usualPorts[l.Port] {
			odd = append(odd, strconv.Itoa(l.Port))
		}
	}
	if len(odd) > 0 {
		add("порт", health.Warn, strings.Join(odd, " "), "открыты наружу необычные порты "+strings.Join(odd, ", ")+": проверь, что это и нужно ли")
	}
	if r.NoSudoSSHD {
		add("sshd", health.Skip, "не проверено", "настройки sshd читаются только через sudo без пароля")
	} else {
		if r.SSHD["permitrootlogin"] == "yes" {
			add("sshd", health.Bad, "root по паролю", "PermitRootLogin yes: вход под root, в том числе по паролю")
		}
		if r.SSHD["passwordauthentication"] == "yes" {
			add("sshd", health.Warn, "вход по паролю", "PasswordAuthentication yes: пароли можно перебирать")
		}
	}
	if r.Fail2ban != "active" && r.UFW != "active" {
		text := "fail2ban не запущен"
		if r.UFW == "" {
			text += ", ufw не проверен"
		}
		add("защита", health.Warn, text, "ни fail2ban, ни ufw не видно: перебор паролей и сканирование ничем не ограничены")
	}
	switch r.Nginx {
	case "", "ok":
	case "nosudo":
		add("nginx", health.Skip, "не проверено", "nginx -t читает сертификаты, без sudo его не запустить")
	default:
		add("nginx", health.Bad, "конфиг с ошибкой", "nginx -t: "+r.Nginx+" - следующий reload не пройдёт")
	}
	for _, c := range r.Certs {
		if d := health.Deadline("сертификат "+c.Name, c.Expires, 21, 7); d.Level > health.OK {
			d.Name = "сертификат"
			out = append(out, d)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Level > out[j].Level })
	return out
}
