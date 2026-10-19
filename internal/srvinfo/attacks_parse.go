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

// Count: сколько раз встретилось значение - адрес, имя, порт.
type Count struct {
	Key string `json:"key"`
	N   int    `json:"n"`
}

// Login: удачный вход по ssh.
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

// Attacks: что видно в журналах сервера за окно. Состояния источников: пусто - прочитан, none - нет
// источника, nosudo - есть, но без прав.
type Attacks struct {
	Since      time.Duration `json:"since"`
	SSHSource  string        `json:"ssh_source"`
	SSHFailed  int           `json:"ssh_failed"`
	SSHIPs     int           `json:"ssh_ips"`
	SSHByIP    []Count       `json:"ssh_by_ip,omitempty"`
	SSHUsers   []Count       `json:"ssh_users,omitempty"`
	Logins     []Login       `json:"logins,omitempty"`
	Probes     []Probe       `json:"probes,omitempty"`
	NginxFiles []string      `json:"nginx_files,omitempty"`
	UFWState   string        `json:"ufw_state,omitempty"`
	UFWBlocked int           `json:"ufw_blocked"`
	UFWByIP    []Count       `json:"ufw_by_ip,omitempty"`
	UFWByPort  []Count       `json:"ufw_by_port,omitempty"`
	F2BState   string        `json:"fail2ban_state,omitempty"`
	Jails      []Jail        `json:"jails,omitempty"`
	CSState    string        `json:"crowdsec_state,omitempty"`
	Decisions  []Decision    `json:"decisions,omitempty"`
	Fail2ban   bool          `json:"fail2ban_active"`
	CrowdSec   bool          `json:"crowdsec_active"`
	Password   string        `json:"password_login"` // yes, no или пусто - не проверено
}

// ParseAttacks собирает Attacks из вывода AttacksScript. Пробы старше окна и запросы самого tim audit
// отбрасываются: своя проверка - не атака.
func ParseAttacks(out string, now time.Time, since time.Duration) Attacks {
	s := Sections(out)
	a := Attacks{Since: since, SSHSource: first(s["sshsrc"])}
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
	for _, l := range s["sshok"] {
		if m := acceptedRe.FindStringSubmatch(l); m != nil {
			a.Logins = append(a.Logins, Login{Time: logTime(strings.Fields(l)[0]), Method: m[1], User: m[2], IP: m[3]})
		}
	}
	a.Probes, a.NginxFiles = parseProbes(s["probes"], now.Add(-since))
	a.UFWState, a.UFWBlocked, a.UFWByIP, a.UFWByPort = parseUFW(s["ufw"])
	a.F2BState, a.Jails = parseJails(s["f2b"])
	a.CSState, a.Decisions = parseDecisions(s["cscli"])
	for _, l := range s["prot"] {
		f := strings.Fields(strings.ToLower(l))
		switch {
		case len(f) == 2 && f[0] == "fail2ban":
			a.Fail2ban = f[1] == "active"
		case len(f) == 2 && f[0] == "crowdsec":
			a.CrowdSec = f[1] == "active"
		case len(f) == 2 && (f[0] == "passwordauthentication" || f[0] == "kbdinteractiveauthentication") && a.Password != "yes":
			a.Password = f[1]
		}
	}
	return a
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

func parseUFW(lines []string) (state string, total int, byIP, byPort []Count) {
	for _, l := range lines {
		f := strings.Fields(l)
		switch {
		case len(f) == 1 && f[0] == "none":
			return "none", 0, nil, nil
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
	return "", total, byIP, byPort
}

// parseJails разбирает fail2ban-client status по клеткам: Currently banned, Total banned, Banned IP list.
func parseJails(lines []string) (string, []Jail) {
	if s := first(lines); s == "none" || s == "nosudo" {
		return s, nil
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
	return "", jails
}

// parseDecisions разбирает cscli decisions list -o json: список тревог, у каждой - решения о бане.
func parseDecisions(lines []string) (string, []Decision) {
	if s := first(lines); s == "none" || s == "nosudo" {
		return s, nil
	}
	var alerts []struct {
		Decisions []struct {
			Value    string `json:"value"`
			Scenario string `json:"scenario"`
			Duration string `json:"duration"`
		} `json:"decisions"`
	}
	if err := json.Unmarshal([]byte(strings.Join(lines, "\n")), &alerts); err != nil {
		return "", nil
	}
	var out []Decision
	for _, al := range alerts {
		for _, d := range al.Decisions {
			out = append(out, Decision{IP: d.Value, Scenario: d.Scenario, Duration: d.Duration})
		}
	}
	return "", out
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
