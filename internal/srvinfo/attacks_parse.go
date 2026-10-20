package srvinfo

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	acceptedRe = regexp.MustCompile(`Accepted (\S+) for (\S+) from (\S+)`)
	accessRe   = regexp.MustCompile(`^(\S+) (\S+) \S+ \S+ \[([^\]]+)\] "([A-Z]+) (\S+)[^"]*" (\d{3})`)
	queryRe    = regexp.MustCompile(`[?#].*$`)
)

// sourceNames: метки скрипта и слова для JSON.
var sourceNames = map[Source]string{Missing: "missing", Read: "read", NoSudo: "nosudo"}

// Source: удалось ли прочитать источник на сервере.
type Source int

const (
	Missing Source = iota // источника нет: не стоит, не включён, нет файла
	Read
	NoSudo // есть, но читает только root
)

func (s Source) MarshalText() ([]byte, error) { return []byte(sourceNames[s]), nil }

// Password: пускает ли sshd по паролю; неизвестно - настройки не прочитаны.
type Password int

const (
	PasswordUnknown Password = iota
	PasswordOn
	PasswordOff
)

func (p Password) MarshalText() ([]byte, error) {
	return []byte([...]string{"unknown", "on", "off"}[p]), nil
}

// Count: сколько раз встретилось значение - адрес, имя, порт.
type Count struct {
	Key string `json:"key"`
	N   int    `json:"n"`
}

// Login: удачный вход по ssh. Time нулевое у старого формата журнала без года.
type Login struct {
	Time   time.Time `json:"time"`
	User   string    `json:"user"`
	IP     string    `json:"ip"`
	Method string    `json:"method"` // password, publickey, keyboard-interactive
}

// Probe: запрос к пути, который ищут боты.
type Probe struct {
	Time   time.Time `json:"time"`
	IP     string    `json:"ip"`
	Path   string    `json:"path"`
	Status int       `json:"status"`
	File   string    `json:"file"`
}

// Jail: клетка fail2ban.
type Jail struct {
	Name   string   `json:"name"`
	Banned int      `json:"banned"`
	Total  int      `json:"total"`
	IPs    []string `json:"ips,omitempty"`
}

// Decision: бан CrowdSec.
type Decision struct {
	IP       string `json:"ip"`
	Scenario string `json:"scenario"`
	Duration string `json:"duration"`
}

// Attacks: что видно в журналах сервера за окно.
type Attacks struct {
	Since      time.Duration `json:"since"`
	SSH        Source        `json:"ssh"`
	SSHVia     string        `json:"ssh_via,omitempty"` // journal или auth.log; у auth.log окно - весь файл
	SSHFailed  int           `json:"ssh_failed"`
	SSHIPs     int           `json:"ssh_ips"`
	SSHByIP    []Count       `json:"ssh_by_ip,omitempty"`
	SSHUsers   []Count       `json:"ssh_users,omitempty"`
	Logins     []Login       `json:"logins,omitempty"`
	Probes     []Probe       `json:"probes,omitempty"`
	NginxFiles []string      `json:"nginx_files,omitempty"`
	UFW        Source        `json:"ufw"`
	UFWBlocked int           `json:"ufw_blocked"`
	UFWByIP    []Count       `json:"ufw_by_ip,omitempty"`
	UFWByPort  []Count       `json:"ufw_by_port,omitempty"`
	F2B        Source        `json:"fail2ban"`
	Jails      []Jail        `json:"jails,omitempty"`
	CS         Source        `json:"crowdsec"`
	Decisions  []Decision    `json:"decisions,omitempty"`
	Fail2ban   bool          `json:"fail2ban_active"`
	CrowdSec   bool          `json:"crowdsec_active"`
	Password   Password      `json:"password_login"`
}

// ParseAttacks собирает Attacks из вывода AttacksScript. Пробы и входы старше окна и запросы самого
// tim audit отбрасываются: своя проверка - не атака.
func ParseAttacks(out string, now time.Time, since time.Duration) Attacks {
	s := Sections(out)
	a := Attacks{Since: since}
	a.SSH, a.SSHVia = sshSource(first(s["sshsrc"]))
	for _, l := range s["sshfail"] {
		if v, ok := strings.CutPrefix(l, "total "); ok {
			a.SSHIPs, _ = strconv.Atoi(strings.TrimSpace(v))
		} else if c, ok := parseCount(l); ok {
			a.SSHByIP = append(a.SSHByIP, c)
		}
	}
	a.SSHFailed, _ = strconv.Atoi(first(s["sshfailtotal"]))
	for _, l := range s["sshusers"] {
		if c, ok := parseCount(l); ok {
			a.SSHUsers = append(a.SSHUsers, c)
		}
	}
	from := now.Add(-since)
	for _, l := range s["sshok"] {
		m := acceptedRe.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		t := logTime(strings.Fields(l)[0])
		if !t.IsZero() && t.Before(from) {
			continue
		}
		a.Logins = append(a.Logins, Login{Time: t, Method: m[1], User: m[2], IP: m[3]})
	}
	a.Probes, a.NginxFiles = parseProbes(s["probes"], from)
	a.UFW, a.UFWBlocked, a.UFWByIP, a.UFWByPort = parseUFW(s["ufw"])
	a.F2B, a.Jails = parseJails(s["f2b"])
	a.CS, a.Decisions = parseDecisions(s["cscli"])
	for _, l := range s["prot"] {
		f := strings.Fields(strings.ToLower(l))
		if len(f) != 2 {
			continue
		}
		switch f[0] {
		case "fail2ban":
			a.Fail2ban = f[1] == "active"
		case "crowdsec":
			a.CrowdSec = f[1] == "active"
		case "passwordauthentication", "kbdinteractiveauthentication":
			// Любой из двух способов "yes" - по паролю пускает.
			if f[1] == "yes" || a.Password == PasswordUnknown {
				a.Password = PasswordOff
				if f[1] == "yes" {
					a.Password = PasswordOn
				}
			}
		}
	}
	return a
}

// sshSource: метка скрипта - journal, journal-sudo, authlog, authlog-sudo или none.
func sshSource(mark string) (Source, string) {
	switch {
	case mark == "none", mark == "":
		return Missing, ""
	case strings.HasPrefix(mark, "authlog"):
		return Read, "auth.log"
	}
	return Read, "journal"
}

func parseProbes(lines []string, from time.Time) ([]Probe, []string) {
	var probes []Probe
	var files []string
	seen := map[string]bool{}
	for _, l := range lines {
		if f, ok := strings.CutPrefix(l, "file "); ok {
			files = append(files, f)
			continue
		}
		m := accessRe.FindStringSubmatch(l)
		if m == nil || strings.Contains(l, "tim-audit") {
			continue
		}
		t, err := time.Parse("02/Jan/2006:15:04:05 -0700", m[3])
		if err == nil && t.Before(from) {
			continue
		}
		status, _ := strconv.Atoi(m[6])
		p := Probe{Time: t, IP: m[2], Path: queryRe.ReplaceAllString(m[5], ""), Status: status, File: m[1]}
		// Один запрос виден и во frontend, и в backend журнале: считается один раз.
		key := m[3] + " " + p.IP + " " + p.Path
		if !seen[key] {
			seen[key] = true
			probes = append(probes, p)
		}
	}
	return probes, files
}

func parseUFW(lines []string) (state Source, total int, byIP, byPort []Count) {
	if first(lines) == "none" {
		return Missing, 0, nil, nil
	}
	for _, l := range lines {
		f := strings.Fields(l)
		switch {
		case len(f) == 2 && f[0] == "total":
			total, _ = strconv.Atoi(f[1])
		case len(f) == 3 && (f[0] == "src" || f[0] == "dpt"):
			n, _ := strconv.Atoi(f[1])
			if f[0] == "src" {
				byIP = append(byIP, Count{f[2], n})
			} else {
				byPort = append(byPort, Count{f[2], n})
			}
		}
	}
	return Read, total, byIP, byPort
}

// parseJails разбирает fail2ban-client status по клеткам: Currently banned, Total banned, Banned IP list.
func parseJails(lines []string) (Source, []Jail) {
	if src, ok := sourceMark(first(lines)); ok {
		return src, nil
	}
	var jails []Jail
	for _, l := range lines {
		if name, ok := strings.CutPrefix(l, "jail "); ok {
			jails = append(jails, Jail{Name: name})
			continue
		}
		if len(jails) == 0 {
			continue
		}
		j := &jails[len(jails)-1]
		_, v, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		switch {
		case strings.Contains(l, "Currently banned"):
			j.Banned, _ = strconv.Atoi(strings.TrimSpace(v))
		case strings.Contains(l, "Total banned"):
			j.Total, _ = strconv.Atoi(strings.TrimSpace(v))
		case strings.Contains(l, "Banned IP list"):
			j.IPs = strings.Fields(v)
		}
	}
	return Read, jails
}

// parseDecisions разбирает cscli decisions list -o json: список тревог, у каждой - решения о бане.
func parseDecisions(lines []string) (Source, []Decision) {
	if src, ok := sourceMark(first(lines)); ok {
		return src, nil
	}
	var alerts []struct {
		Decisions []struct {
			Value    string `json:"value"`
			Scenario string `json:"scenario"`
			Duration string `json:"duration"`
		} `json:"decisions"`
	}
	if err := json.Unmarshal([]byte(strings.Join(lines, "\n")), &alerts); err != nil {
		return Read, nil
	}
	var out []Decision
	for _, al := range alerts {
		for _, d := range al.Decisions {
			out = append(out, Decision{IP: d.Value, Scenario: d.Scenario, Duration: d.Duration})
		}
	}
	return Read, out
}

// sourceMark: первая строка раздела - метка none или nosudo, а не данные.
func sourceMark(line string) (Source, bool) {
	switch line {
	case "none", "":
		return Missing, true
	case "nosudo":
		return NoSudo, true
	}
	return Read, false
}

// parseCount: строка uniq -c - "  123 значение".
func parseCount(l string) (Count, bool) {
	f := strings.Fields(l)
	if len(f) != 2 {
		return Count{}, false
	}
	n, err := strconv.Atoi(f[0])
	if err != nil {
		return Count{}, false
	}
	return Count{Key: f[1], N: n}, true
}

// logTime: время строки journalctl -o short-iso или rsyslog в RFC 3339; старый формат без года - нулевое.
func logTime(s string) time.Time {
	for _, layout := range []string{"2006-01-02T15:04:05-0700", time.RFC3339Nano} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func first(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.TrimSpace(lines[0])
}
