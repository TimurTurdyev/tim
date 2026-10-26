package srvinfo

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/cronx"
	"github.com/TimurTurdyev/tim/internal/logsum"
	"github.com/TimurTurdyev/tim/internal/shellq"
)

// prelude: S - sudo -n, если он есть без пароля; IO - ionice, если есть.
const prelude = `export LC_ALL=C
S=; sudo -n true 2>/dev/null && S="sudo -n"
IO=; command -v ionice >/dev/null 2>&1 && IO="ionice -c3"
`

// limited: журналы видны не все - нет sudo без пароля и групп adm, systemd-journal.
const limited = `[ -z "$S" ] && ! id -nG | grep -qwE 'adm|systemd-journal' && echo @@limited
`

// unitName: имя юнита без кавычек и пробелов - в скрипт уходит только такое.
var unitName = regexp.MustCompile(`^[A-Za-z0-9@._:-]+$`)

// Recipes: встроенные заготовки по порядку палитры.
var Recipes = []Recipe{
	{ID: "disk", Name: "что растёт на диске", Desc: "du по /var /home /srv /opt, 20 крупнейших", Next: []string{"big", "docker"},
		script: `$S nice -n 19 $IO du -xh --max-depth=2 /var /home /srv /opt /root 2>/dev/null | sort -rh | head -20`},
	{ID: "big", Name: "крупные файлы", Desc: "больше 500 MB на корневом разделе", Next: []string{"disk"},
		script: `$S nice -n 19 $IO find / -xdev -type f -size +500M -printf '%s %TY-%Tm-%Td %p\n' 2>/dev/null | sort -rn | head -20`,
		post:   humanSizes},
	{ID: "errors", Name: "ошибки журнала за сутки", Desc: "одинаковые строки сведены, счётчик и время", Next: []string{"oom", "unit"},
		script: limited + `command -v journalctl >/dev/null 2>&1 || { echo "journalctl нет на сервере"; exit 0; }
$S journalctl -p err --since "24 hours ago" --no-pager -o short-iso -q 2>/dev/null | tail -n 20000`,
		post: summarize},
	{ID: "oom", Name: "кого убил OOM", Desc: "журнал ядра за неделю: кого и когда", Next: []string{"top"},
		script: limited + `{ $S journalctl -k --since "7 days ago" --no-pager -o short-iso -q 2>/dev/null || $S dmesg -T 2>/dev/null; } |
grep -iE 'out of memory|oom-kill|killed process' | tail -30`,
		post: func(out string) string {
			if strings.TrimSpace(out) == "" {
				return "за неделю OOM никого не убивал\n"
			}
			return out
		}},
	{ID: "unit", Name: "юнит", Desc: "systemctl status и 50 строк его журнала", Param: "юнит", Next: []string{"errors"},
		script: limited + `systemctl status --no-pager -n 0 {param} 2>&1
echo; echo "журнал:"
$S journalctl -u {param} -n 50 --no-pager -o short-iso -q 2>&1`},
	{ID: "top", Name: "кто держит память и CPU", Desc: "ps по памяти и по процессору", Next: []string{"oom", "ports"},
		script: `echo "по памяти:"; ps -eo pid,user,rss,%mem,%cpu,etime,comm --sort=-rss | head -11
echo; echo "по процессору:"; ps -eo pid,user,%cpu,%mem,etime,comm --sort=-%cpu | head -11`},
	{ID: "ports", Name: "порты с процессами", Desc: "ss -tlnp: кто слушает и на каком адресе", Next: []string{"who"},
		script: `$S ss -tlnpH 2>/dev/null || ss -tlnH`, post: listenPorts},
	{ID: "who", Name: "кто подключён сейчас", Desc: "сессии, соединения по адресам, последние входы", Next: []string{"ports"},
		script: `echo "сессии:"; who
echo; echo "соединения по адресам:"; ss -tnH state established 2>/dev/null | awk '{print $4}' | sed 's/:[0-9]*$//' | sort | uniq -c | sort -rn | head -15
echo; echo "последние входы:"; last -n 10 -a 2>/dev/null | head -10`},
	{ID: "cron", Name: "крон и таймеры", Desc: "crontab, /etc/cron.d, systemd-таймеры", Next: []string{"errors"},
		script: `echo "crontab $(id -un):"; crontab -l 2>/dev/null | grep -vE '^\s*(#|$)'
for f in /etc/crontab /etc/cron.d/*; do [ -f "$f" ] || continue; echo; echo "$f:"; grep -vE '^\s*(#|$)|^[A-Z_]+=' "$f"; done
$S sh -c 'for f in /var/spool/cron/crontabs/*; do [ -f "$f" ] || continue; echo; echo "$f:"; grep -vE "^\s*(#|$)" "$f"; done' 2>/dev/null
echo; echo "таймеры:"; systemctl list-timers --no-pager 2>/dev/null | head -25`,
		post: explainCron},
	{ID: "docker", Name: "контейнеры", Desc: "docker ps, stats, место под образы", Next: []string{"disk"},
		script: `command -v docker >/dev/null 2>&1 || { echo "docker нет на сервере"; exit 0; }
D=docker; docker info >/dev/null 2>&1 || D="$S docker"
$D ps -a --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}' 2>&1
echo; $D stats --no-stream --format 'table {{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}' 2>&1
echo; $D system df 2>&1`},
	{ID: "sites", Name: "сайты nginx", Desc: "имя, корень и сертификат, хвост error.log", Next: []string{"errors"},
		script: `command -v nginx >/dev/null 2>&1 || { echo "nginx нет на сервере"; exit 0; }
cd /etc/nginx 2>/dev/null || exit 0
[ -z "$S" ] && [ "$(id -u)" != 0 ] && echo @@nosudo
F="sites-enabled/* conf.d/*.conf $(sed -n 's/^[[:space:]]*include[[:space:]]\{1,\}\([^;]*\);.*/\1/p' nginx.conf 2>/dev/null)"
for f in $F; do [ -f "$f" ] && echo "$f"; done | sort -u | while read -r f; do $S grep -HE '^\s*(server_name|root|ssl_certificate|set)\s' "$f" 2>/dev/null; done
echo "@@log"; $S tail -n 15 /var/log/nginx/error.log 2>/dev/null`,
		post: nginxSites},
}

var procName = regexp.MustCompile(`\("([^"]+)",pid`)

// Recipe: заготовка для сервера - короткий скрипт только на чтение, стандартный Linux без допущений
// о раскладке. Тяжёлое (du, find) идёт под nice и ionice; root - через sudo -n, без пароля он отказывает,
// и заготовка показывает то, что видно пользователю.
type Recipe struct {
	ID, Name, Desc string
	Param          string   // что подставляется вторым аргументом: "юнит"; пусто - без параметра
	Next           []string // что логично открыть после
	Own            bool     // своя, из конфига: проверить на "только чтение" её нельзя
	script         string   // {param} заменяется экранированным параметром
	post           func(out string) string
}

// FindRecipe: заготовка по имени среди встроенных и своих.
func FindRecipe(id string, own []Recipe) (Recipe, bool) {
	for _, r := range append(append([]Recipe{}, Recipes...), own...) {
		if r.ID == id {
			return r, true
		}
	}
	return Recipe{}, false
}

// OwnRecipe: своя заготовка из конфига; команда уходит на сервер как есть, {unit} - параметр.
func OwnRecipe(id, desc, command string) Recipe {
	r := Recipe{ID: id, Name: id, Desc: desc, Own: true, script: strings.ReplaceAll(command, "{unit}", "{param}")}
	if r.script != command {
		r.Param = "юнит"
	}
	return r
}

// Script: что уйдёт на сервер - для --dry-run и окна подтверждения.
func (r Recipe) Script(param string) (string, error) {
	if r.Param != "" {
		if !unitName.MatchString(param) {
			return "", fmt.Errorf("%s: нужно имя, например nginx или php8.3-fpm", r.Param)
		}
	} else if param != "" {
		return "", fmt.Errorf("заготовке %s параметр не нужен", r.ID)
	}
	body := strings.ReplaceAll(r.script, "{param}", shellq.Quote(param))
	// Тело в подоболочке: его exit не должен съесть маркер конца.
	return prelude + "(\n" + body + "\n)\necho @@end\n", nil
}

// RunRecipe: заготовка на сервере по ssh, вывод уже разобран.
func RunRecipe(target string, r Recipe, param string) (string, error) {
	script, err := r.Script(param)
	if err != nil {
		return "", err
	}
	out, err := remote(target, script, 15*time.Second, 2*time.Minute)
	out, _, _ = strings.Cut(out, "@@end")
	out, partial := strings.CutPrefix(out, "@@limited\n")
	if r.post != nil && err == nil {
		out = r.post(out)
	}
	if strings.TrimSpace(out) == "" && err == nil {
		out = "пусто\n"
	}
	if partial {
		out += "\nвидно не всё: журналы системы читают root и группы adm, systemd-journal, а sudo -n просит пароль\n"
	}
	return out, err
}

// summarize: одинаковые строки журнала одной строкой со счётчиком.
func summarize(out string) string {
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	pats := logsum.Summarize(lines)
	if len(pats) == 0 {
		return "за сутки ошибок в журнале нет\n"
	}
	return logsum.Format(pats, len(lines))
}

// humanSizes: размер в байтах первой колонкой - в MB и GB.
func humanSizes(out string) string {
	var b strings.Builder
	for _, l := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		size, rest, ok := strings.Cut(l, " ")
		var n float64
		if _, err := fmt.Sscan(size, &n); !ok || err != nil {
			b.WriteString(l + "\n")
			continue
		}
		fmt.Fprintf(&b, "%7.1f GB  %s\n", n/(1<<30), rest)
	}
	return b.String()
}

// listenPorts: вывод ss -tlnp коротко - порт, адрес, процессы; наружу или только на этой машине.
func listenPorts(out string) string {
	type row struct {
		port        int
		addr, procs string
	}
	var rows []row
	seen := map[string]bool{}
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) < 4 {
			continue
		}
		local := f[3]
		i := strings.LastIndex(local, ":")
		if i < 0 {
			continue
		}
		var port int
		if _, err := fmt.Sscan(local[i+1:], &port); err != nil {
			continue
		}
		addr := strings.Trim(local[:i], "[]")
		var names []string
		for _, m := range procName.FindAllStringSubmatch(l, -1) {
			if !slices.Contains(names, m[1]) {
				names = append(names, m[1])
			}
		}
		key := fmt.Sprint(port, addr)
		if seen[key] {
			continue
		}
		seen[key] = true
		rows = append(rows, row{port, addr, strings.Join(names, ", ")})
	}
	slices.SortStableFunc(rows, func(a, b row) int { return a.port - b.port })
	var b strings.Builder
	for _, r := range rows {
		where := "наружу"
		if r.addr == "127.0.0.1" || r.addr == "::1" || strings.HasPrefix(r.addr, "127.") || strings.HasSuffix(r.addr, "%lo") {
			where = "только здесь"
		}
		fmt.Fprintf(&b, "%6d  %-13s %-22s %s\n", r.port, where, r.addr, r.procs)
	}
	return b.String()
}

// explainCron: у строк крона - расписание словами, как в tim cron.
func explainCron(out string) string {
	var b strings.Builder
	for _, l := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		f := strings.Fields(l)
		if len(f) > 5 {
			if s, err := cronx.Parse(strings.Join(f[:5], " ")); err == nil {
				l += "   # " + s.Describe()
			}
		}
		b.WriteString(l + "\n")
	}
	return b.String()
}

// nginxSites: строки конфигов nginx по файлам - имя, корень, сертификат; ниже хвост error.log.
func nginxSites(out string) string {
	conf, log, _ := strings.Cut(out, "@@log")
	conf, nosudo := strings.CutPrefix(conf, "@@nosudo\n")
	type site struct {
		file, names, root, cert string
		vars                    map[string]string
	}
	var sites []*site
	byFile := map[string]*site{}
	for _, l := range strings.Split(conf, "\n") {
		file, line, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		f := strings.Fields(strings.ReplaceAll(strings.TrimSuffix(strings.TrimSpace(line), ";"), `"`, ""))
		if len(f) < 2 {
			continue
		}
		s := byFile[file]
		if s == nil {
			s = &site{file: file, vars: map[string]string{}}
			byFile[file] = s
			sites = append(sites, s)
		}
		switch f[0] {
		case "server_name":
			if s.names == "" {
				s.names = strings.Join(f[1:], " ")
			}
		case "root":
			s.root = f[1]
			if v, ok := s.vars[f[1]]; ok {
				s.root = v
			}
		case "ssl_certificate":
			s.cert = f[1]
		case "set":
			if len(f) > 2 {
				s.vars[f[1]] = f[2]
			}
		}
	}
	var b strings.Builder
	for _, s := range sites {
		if s.names == "" || s.names == "_" || strings.HasPrefix(s.names, "-") {
			continue
		}
		fmt.Fprintf(&b, "%s\n", s.names)
		if s.root != "" {
			fmt.Fprintf(&b, "  корень       %s\n", s.root)
		}
		if s.cert != "" {
			fmt.Fprintf(&b, "  сертификат   %s\n", s.cert)
		}
		fmt.Fprintf(&b, "  конфиг       /etc/nginx/%s\n", strings.TrimPrefix(s.file, "/etc/nginx/"))
	}
	switch {
	case b.Len() == 0 && nosudo:
		b.WriteString("сайтов не видно: конфиги сайтов часто читает только root, а sudo -n просит пароль\n")
	case b.Len() == 0:
		b.WriteString("сайтов в конфигах nginx не нашёл\n")
	}
	if log = strings.TrimSpace(log); log != "" {
		b.WriteString("\nerror.log, последние строки:\n" + log + "\n")
	}
	return b.String()
}
