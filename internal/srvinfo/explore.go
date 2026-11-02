package srvinfo

import (
	"cmp"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// exploreScript: сайты и пользователи сервера одним заходом, только чтение. Конфиг nginx целиком даёт nginx -T;
// корни, сертификаты и журналы считываются по нему же, запросы за сутки считает awk на сервере - по ssh едут
// числа, а не журналы.
const exploreScript = `export LC_ALL=C
S=; [ "$(id -u)" = 0 ] || { sudo -n true 2>/dev/null && S="sudo -n" || echo @@nosudo; }
N=$(mktemp 2>/dev/null || echo /tmp/.tim-nginx.$$)
command -v nginx >/dev/null 2>&1 && $S nginx -T 2>/dev/null | grep -E '^# configuration file |^[[:space:]]*server[[:space:]]*\{|^[[:space:]]*(server_name|root|ssl_certificate|access_log|error_log|fastcgi_pass|proxy_pass|set)[[:space:]]' > "$N"
echo @@nginx; cat "$N" 2>/dev/null
echo @@dirs
awk '($1=="root" || $1=="set") {v=$NF; gsub(/[;"]/,"",v); if (v ~ /^\//) print v}' "$N" 2>/dev/null | sort -u | while IFS= read -r d; do
  [ -d "$d" ] && echo "$d|$($S stat -c %U "$d" 2>/dev/null)|$($S find "$d" -maxdepth 1 -printf '%T@\n' 2>/dev/null | sort -n | tail -1)"
done
echo @@certs
awk '$1=="ssl_certificate" {v=$2; gsub(/[;"]/,"",v); print v}' "$N" 2>/dev/null | sort -u | while IFS= read -r c; do
  echo "$c|$($S openssl x509 -enddate -noout -in "$c" 2>/dev/null | cut -d= -f2)"
done
echo @@hits
T=$(date +%d/%b/%Y); Y=$(date -d yesterday +%d/%b/%Y 2>/dev/null); H=$(date +%H)
awk '$1=="access_log" {v=$2; gsub(/[;"]/,"",v); if (v ~ /^\//) print v}' "$N" 2>/dev/null | sort -u | while IFS= read -r f; do
  $S test -f "$f" || continue
  $S tail -n 200000 "$f" 2>/dev/null | nice awk -v T="$T" -v Y="$Y" -v H="$H" -v F="$f" '{i=index($0,"["); d=substr($0,i+1,11); h=substr($0,i+13,2); if (d==T || (d==Y && h>=H)) {n++; split($0,q,"\""); split(q[3],s," "); if (s[1] ~ /^5/) e++}} END {print F "|" n+0 "|" e+0}'
done
rm -f "$N"
echo @@php; $S grep -HE '^[[:space:]]*listen[[:space:]]*=' /etc/php/*/fpm/pool.d/*.conf /opt/php*/etc/php-fpm.d/*.conf /etc/php-fpm.d/*.conf 2>/dev/null
echo @@users; awk -F: '($3==0 || ($3>=1000 && $3<65534)) {print $1 "|" $3 "|" $6 "|" $7}' /etc/passwd
echo @@groups; awk -F: '$3>=1000 && $3<65534 {print $1}' /etc/passwd | while read -r u; do echo "$u|$(id -nG "$u" 2>/dev/null)"; done
echo @@created; ( $S zgrep -h "new user: name=" /var/log/auth.log* /var/log/secure* 2>/dev/null; $S journalctl -q --no-pager -o short-iso _COMM=useradd 2>/dev/null | grep "new user: name=" ) | tail -200
echo @@lastlog; lastlog 2>/dev/null | tail -n +2 | grep -v 'Never logged in'
echo @@keys; awk -F: '($3==0 || ($3>=1000 && $3<65534)) {print $1 "|" $6}' /etc/passwd | while IFS='|' read -r u h; do f="$h/.ssh/authorized_keys"; if $S test -f "$f"; then echo "$u|$($S grep -cE '^[[:space:]]*(ssh-|ecdsa-|sk-)' "$f" 2>/dev/null)"; else echo "$u|0"; fi; done
echo @@procs; ps -eo user= 2>/dev/null | sort | uniq -c
echo @@panel; [ -d /usr/local/fastpanel2 ] && echo fastpanel
echo @@end
`

var (
	phpPathRe  = regexp.MustCompile(`/php/(\d+\.\d+)/|/opt/php(\d)(\d+)/`)
	newUserRe  = regexp.MustCompile(`new user: name=([^,\s]+)`)
	lastlogLay = "Mon Jan 2 15:04:05 -0700 2006"
)

// Site: сайт сервера - из конфига nginx и того, что лежит на диске.
type Site struct {
	Name     string    `json:"name"`
	Aliases  []string  `json:"aliases,omitempty"`
	Conf     string    `json:"conf"`
	Root     string    `json:"root,omitempty"`
	Owner    string    `json:"owner,omitempty"`
	Touched  time.Time `json:"touched,omitzero"` // самое позднее изменение в корне и его первом уровне
	PHP      string    `json:"php,omitempty"`    // версия PHP-FPM, которой отдан сайт
	Proxy    string    `json:"proxy,omitempty"`  // куда проксирует, если сайт - приложение за nginx
	Cert     string    `json:"cert,omitempty"`   // файл сертификата
	CertEnd  time.Time `json:"cert_end,omitzero"`
	Logs     []string  `json:"logs,omitempty"` // журналы запросов
	ErrorLog string    `json:"error_log,omitempty"`
	Hits     int       `json:"hits"`   // запросов за сутки
	Errors   int       `json:"errors"` // ответов 5xx за сутки
	sockets  []string
}

// Account: пользователь сервера - кем и когда заведён, чем владеет, входил ли.
type Account struct {
	Name      string    `json:"name"`
	UID       int       `json:"uid"`
	Home      string    `json:"home"`
	Shell     string    `json:"shell"`
	Groups    []string  `json:"groups,omitempty"`
	Created   time.Time `json:"created,omitzero"`
	By        string    `json:"by,omitempty"` // кто завёл: FASTPANEL, панель хостинга, система
	Keys      int       `json:"keys"`         // ключей ssh в authorized_keys
	LastLogin time.Time `json:"last_login,omitzero"`
	LastFrom  string    `json:"last_from,omitempty"`
	Procs     int       `json:"procs"`
	Sites     []string  `json:"sites,omitempty"`
}

// Explore: сайты и пользователи сервера.
type Explore struct {
	Sites    []Site    `json:"sites"`
	Accounts []Account `json:"accounts"`
	Panel    string    `json:"panel,omitempty"`
	NoSudo   bool      `json:"no_sudo,omitempty"` // без root часть конфигов и журналов не видна
}

// ExploreScript: скрипт обхода сайтов и пользователей.
func ExploreScript() string { return exploreScript }

// ParseExplore: вывод скрипта обхода; now - для года в старом формате журнала.
func ParseExplore(out string, now time.Time) Explore {
	sec := sections(out)
	e := Explore{NoSudo: strings.Contains(out, "@@nosudo")}
	if len(sec["panel"]) > 0 {
		e.Panel = "FASTPANEL"
	}
	e.Sites = parseSites(sec)
	e.Accounts = parseAccounts(sec, now)
	for i := range e.Accounts {
		a := &e.Accounts[i]
		a.By = creator(*a, e.Panel)
		for _, s := range e.Sites {
			if s.Owner == a.Name || a.UID != 0 && s.Root != "" && strings.HasPrefix(s.Root, a.Home+"/") {
				a.Sites = append(a.Sites, s.Name)
			}
		}
	}
	return e
}

// sections: строки по разделам @@имя.
func sections(out string) map[string][]string {
	sec := map[string][]string{}
	cur := ""
	for _, l := range strings.Split(out, "\n") {
		if name, ok := strings.CutPrefix(l, "@@"); ok {
			cur = strings.TrimSpace(name)
			continue
		}
		if cur != "" && strings.TrimSpace(l) != "" {
			sec[cur] = append(sec[cur], l)
		}
	}
	return sec
}

// parseSites: сервер-блоки nginx в сайты. Блоки одного имени сливаются (перенаправление с 80 и сам сайт на 443),
// www.имя - псевдоним имени.
func parseSites(sec map[string][]string) []Site {
	type block struct {
		file  string
		names []string
		vars  map[string]string
		site  Site
	}
	var blocks []*block
	var cur *block
	file := ""
	for _, l := range sec["nginx"] {
		if f, ok := strings.CutPrefix(l, "# configuration file "); ok {
			file, cur = strings.TrimSuffix(f, ":"), nil
			continue
		}
		f := strings.Fields(strings.NewReplacer(";", " ", `"`, "").Replace(l))
		if len(f) == 0 {
			continue
		}
		if f[0] == "server" {
			cur = &block{file: file, vars: map[string]string{}, site: Site{Conf: file}}
			blocks = append(blocks, cur)
			continue
		}
		if cur == nil || len(f) < 2 {
			continue
		}
		s := &cur.site
		switch f[0] {
		case "server_name":
			for _, n := range f[1:] {
				if n != "_" && !strings.HasPrefix(n, "-") {
					cur.names = append(cur.names, n)
				}
			}
		case "set":
			if len(f) > 2 {
				cur.vars[f[1]] = f[2]
			}
		case "root":
			if s.Root == "" {
				s.Root = cmp.Or(cur.vars[f[1]], f[1])
			}
		case "ssl_certificate":
			s.Cert = f[1]
		case "access_log":
			if f[1] != "off" && !slices.Contains(s.Logs, f[1]) {
				s.Logs = append(s.Logs, f[1])
			}
		case "error_log":
			s.ErrorLog = f[1]
		case "fastcgi_pass":
			s.sockets = append(s.sockets, strings.TrimPrefix(f[1], "unix:"))
		case "proxy_pass":
			s.Proxy = f[1]
		}
	}
	byName := map[string]*Site{}
	var order []string
	for _, b := range blocks {
		if len(b.names) == 0 {
			continue
		}
		name := b.names[0]
		if bare, ok := strings.CutPrefix(name, "www."); ok && byName[bare] != nil {
			byName[bare].Aliases = appendNew(byName[bare].Aliases, name)
			continue
		}
		s := byName[name]
		if s == nil {
			s = &Site{Name: name, Conf: b.file}
			byName[name] = s
			order = append(order, name)
		}
		for _, n := range b.names[1:] {
			s.Aliases = appendNew(s.Aliases, n)
		}
		merge(s, b.site)
	}
	dirs := pipeRows(sec["dirs"])
	certs := pipeRows(sec["certs"])
	hits := pipeRows(sec["hits"])
	php := phpVersions(sec["php"])
	out := make([]Site, 0, len(order))
	for _, n := range order {
		s := *byName[n]
		if d, ok := dirs[s.Root]; ok && len(d) >= 2 {
			s.Owner = d[0]
			if sec, err := strconv.ParseFloat(d[1], 64); err == nil {
				s.Touched = time.Unix(int64(sec), 0)
			}
		}
		if c, ok := certs[s.Cert]; ok && len(c) >= 1 {
			s.CertEnd, _ = time.Parse("Jan _2 15:04:05 2006 MST", strings.Join(strings.Fields(c[0]), " "))
		}
		for _, l := range s.Logs {
			if h, ok := hits[l]; ok && len(h) >= 2 {
				n, _ := strconv.Atoi(h[0])
				e, _ := strconv.Atoi(h[1])
				s.Hits += n
				s.Errors += e
			}
		}
		for _, sock := range s.sockets {
			if v, ok := php[filepath.Base(sock)]; ok {
				s.PHP = v
				break
			}
		}
		out = append(out, s)
	}
	return out
}

// merge: пустые поля сайта - из следующего блока того же имени.
func merge(s *Site, b Site) {
	s.Root = cmp.Or(s.Root, b.Root)
	s.Cert = cmp.Or(s.Cert, b.Cert)
	s.ErrorLog = cmp.Or(s.ErrorLog, b.ErrorLog)
	s.Proxy = cmp.Or(s.Proxy, b.Proxy)
	for _, l := range b.Logs {
		s.Logs = appendNew(s.Logs, l)
	}
	s.sockets = append(s.sockets, b.sockets...)
}

// phpVersions: сокет пула PHP-FPM -> версия по пути конфига: /etc/php/8.3/... или /opt/php74/...
func phpVersions(lines []string) map[string]string {
	out := map[string]string{}
	for _, l := range lines {
		file, listen, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		_, sock, _ := strings.Cut(listen, "=")
		m := phpPathRe.FindStringSubmatch(file)
		if m == nil {
			continue
		}
		v := m[1]
		if v == "" {
			v = m[2] + "." + m[3]
		}
		out[filepath.Base(strings.TrimSpace(sock))] = v
	}
	return out
}

func parseAccounts(sec map[string][]string, now time.Time) []Account {
	var out []Account
	for _, l := range sec["users"] {
		f := strings.Split(l, "|")
		if len(f) < 4 {
			continue
		}
		uid, _ := strconv.Atoi(f[1])
		out = append(out, Account{Name: f[0], UID: uid, Home: f[2], Shell: f[3]})
	}
	groups := pipeRows(sec["groups"])
	keys := pipeRows(sec["keys"])
	procs := map[string]int{}
	for _, l := range sec["procs"] {
		if f := strings.Fields(l); len(f) == 2 {
			procs[f[1]], _ = strconv.Atoi(f[0])
		}
	}
	created := map[string]time.Time{}
	for _, l := range sec["created"] {
		m := newUserRe.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		if t := logStamp(l, now); !t.IsZero() && (created[m[1]].IsZero() || t.Before(created[m[1]])) {
			created[m[1]] = t
		}
	}
	last := map[string][2]string{}
	for _, l := range sec["lastlog"] {
		f := strings.Fields(l)
		if len(f) < 7 {
			continue
		}
		from := ""
		if len(f) >= 9 {
			from = f[len(f)-7]
		}
		last[f[0]] = [2]string{strings.Join(f[len(f)-6:], " "), from}
	}
	for i := range out {
		a := &out[i]
		if g, ok := groups[a.Name]; ok && len(g) > 0 {
			a.Groups = strings.Fields(g[0])
		}
		if k, ok := keys[a.Name]; ok && len(k) > 0 {
			a.Keys, _ = strconv.Atoi(k[0])
		}
		a.Procs = procs[a.Name]
		a.Created = created[a.Name]
		if l, ok := last[a.Name]; ok {
			a.LastLogin, _ = time.Parse(lastlogLay, l[0])
			a.LastFrom = l[1]
		}
	}
	return out
}

// creator: кто завёл пользователя - по следам, которые оставляет панель.
func creator(a Account, panel string) string {
	switch {
	case a.UID == 0:
		return "система"
	case panel != "" && slices.Contains(a.Groups, "fastsecure"):
		return panel
	case strings.HasPrefix(a.Home, "/var/www/") && strings.HasSuffix(a.Home, "/data"):
		return "панель хостинга"
	}
	return ""
}

// logStamp: время строки журнала - ISO в начале (rsyslog, journalctl -o short-iso) или "Oct 30 19:29:13" без года.
func logStamp(l string, now time.Time) time.Time {
	f := strings.Fields(l)
	if len(f) == 0 {
		return time.Time{}
	}
	for _, lay := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05-0700"} {
		if t, err := time.Parse(lay, f[0]); err == nil {
			return t
		}
	}
	if len(f) >= 3 {
		if t, err := time.ParseInLocation("Jan 2 15:04:05 2006", f[0]+" "+f[1]+" "+f[2]+" "+strconv.Itoa(now.Year()), now.Location()); err == nil {
			if t.After(now) {
				t = t.AddDate(-1, 0, 0)
			}
			return t
		}
	}
	return time.Time{}
}

// pipeRows: строки "ключ|a|b" -> ключ: [a b].
func pipeRows(lines []string) map[string][]string {
	out := map[string][]string{}
	for _, l := range lines {
		f := strings.Split(l, "|")
		if len(f) > 1 {
			out[f[0]] = f[1:]
		}
	}
	return out
}

func appendNew(list []string, s string) []string {
	if slices.Contains(list, s) {
		return list
	}
	return append(list, s)
}
