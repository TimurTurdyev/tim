package srvinfo

import (
	"net"
	"strconv"
	"strings"
	"time"
)

// Disk: точка монтирования с занятым местом или inode.
type Disk struct {
	Mount string
	Size  int64 // байты для места, штуки для inode
	Used  int64
	Pct   int
}

// Listen: порт, который слушают, и доступен ли он не только с самой машины.
type Listen struct {
	Addr   string
	Port   int
	Public bool
}

// Cert: сертификат Let's Encrypt на диске.
type Cert struct {
	Name    string
	Expires time.Time
}

// Report: всё, что удалось узнать о сервере. NoSudo* - проверка не сделана, а не "всё хорошо".
type Report struct {
	OS                  string
	Uptime              time.Duration
	Cores               int
	Load                [3]float64
	MemTotal, MemAvail  int64
	SwapTotal, SwapFree int64
	Disks, Inodes       []Disk
	Failed              []string
	Reboot              bool
	RebootPkgs          []string
	SecurityUpdates     []string
	Listening           []Listen
	SSHD                map[string]string
	NoSudoSSHD          bool
	UFW                 string
	Fail2ban            string
	Nginx               string // "ok", текст ошибки, "nosudo" или пусто, если nginx нет
	Certs               []Cert
}

// Sections режет вывод Script по маркерам @@имя.
func Sections(out string) map[string][]string {
	m := map[string][]string{}
	cur := ""
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if name, ok := strings.CutPrefix(line, "@@"); ok {
			cur = name
			m[cur] = nil
			continue
		}
		if cur != "" && strings.TrimSpace(line) != "" {
			m[cur] = append(m[cur], line)
		}
	}
	return m
}

// Parse собирает Report из вывода Script.
func Parse(out string) Report {
	s := Sections(out)
	r := Report{SSHD: map[string]string{}}
	for _, l := range s["os"] {
		if v, ok := strings.CutPrefix(l, "PRETTY_NAME="); ok {
			r.OS = strings.Trim(v, `"`)
		}
	}
	if u := s["uptime"]; len(u) >= 3 {
		if f := strings.Fields(u[0]); len(f) > 0 {
			sec, _ := strconv.ParseFloat(f[0], 64)
			r.Uptime = time.Duration(sec) * time.Second
		}
		r.Cores, _ = strconv.Atoi(strings.TrimSpace(u[1]))
		for i, f := range strings.Fields(u[2]) {
			if i < 3 {
				r.Load[i], _ = strconv.ParseFloat(f, 64)
			}
		}
	}
	for _, l := range s["mem"] {
		f := strings.Fields(l)
		if len(f) < 2 {
			continue
		}
		kb, _ := strconv.ParseInt(f[1], 10, 64)
		switch strings.TrimSuffix(f[0], ":") {
		case "MemTotal":
			r.MemTotal = kb * 1024
		case "MemAvailable":
			r.MemAvail = kb * 1024
		case "SwapTotal":
			r.SwapTotal = kb * 1024
		case "SwapFree":
			r.SwapFree = kb * 1024
		}
	}
	r.Disks = parseDf(s["df"], 1024)
	r.Inodes = parseDf(s["inodes"], 1)
	for _, l := range s["failed"] {
		if f := strings.Fields(l); len(f) > 0 {
			r.Failed = append(r.Failed, f[0])
		}
	}
	if rb := s["reboot"]; len(rb) > 0 && rb[0] == "yes" {
		r.Reboot = true
		r.RebootPkgs = uniq(rb[1:])
	}
	r.SecurityUpdates = uniq(s["updates"])
	for _, l := range s["ports"] {
		if ln, ok := parseSS(l); ok {
			r.Listening = append(r.Listening, ln)
		}
	}
	r.Listening = uniqListen(r.Listening)
	for _, l := range s["sshd"] {
		if l == "nosudo" {
			r.NoSudoSSHD = true
			continue
		}
		if f := strings.Fields(l); len(f) == 2 {
			r.SSHD[strings.ToLower(f[0])] = strings.ToLower(f[1])
		}
	}
	for _, l := range s["firewall"] {
		switch {
		case strings.HasPrefix(l, "Status:"):
			r.UFW = strings.TrimSpace(strings.TrimPrefix(l, "Status:"))
		case strings.HasPrefix(l, "fail2ban"):
			r.Fail2ban = strings.TrimSpace(strings.TrimPrefix(l, "fail2ban"))
		}
	}
	if n := s["nginx"]; len(n) > 0 {
		last := n[len(n)-1]
		switch {
		case strings.Contains(last, "password is required") || last == "nosudo":
			r.Nginx = "nosudo"
		case strings.Contains(last, "test is successful") || strings.Contains(last, "syntax is ok"):
			r.Nginx = "ok"
		default:
			r.Nginx = last
		}
	}
	for _, l := range s["certs"] {
		path, rest, ok := strings.Cut(l, " notAfter=")
		if !ok {
			continue
		}
		t, err := time.Parse("Jan _2 15:04:05 2006 MST", strings.TrimSpace(rest))
		if err != nil {
			continue
		}
		parts := strings.Split(path, "/")
		name := path
		if len(parts) >= 2 {
			name = parts[len(parts)-2]
		}
		r.Certs = append(r.Certs, Cert{Name: name, Expires: t})
	}
	return r
}

func parseDf(lines []string, unit int64) []Disk {
	var out []Disk
	for i, l := range lines {
		f := strings.Fields(l)
		if i == 0 || len(f) < 6 {
			continue
		}
		size, err1 := strconv.ParseInt(f[1], 10, 64)
		used, err2 := strconv.ParseInt(f[2], 10, 64)
		pct, err3 := strconv.Atoi(strings.TrimSuffix(f[4], "%"))
		if err1 != nil || err2 != nil || err3 != nil || size == 0 {
			continue
		}
		out = append(out, Disk{Mount: f[5], Size: size * unit, Used: used * unit, Pct: pct})
	}
	return out
}

// parseSS разбирает строку ss -tlnH: State Recv-Q Send-Q Local:Port Peer:Port.
func parseSS(line string) (Listen, bool) {
	f := strings.Fields(line)
	if len(f) < 4 {
		return Listen{}, false
	}
	local := f[3]
	i := strings.LastIndex(local, ":")
	if i < 0 {
		return Listen{}, false
	}
	port, err := strconv.Atoi(local[i+1:])
	if err != nil {
		return Listen{}, false
	}
	addr := strings.Trim(local[:i], "[]")
	if j := strings.Index(addr, "%"); j >= 0 {
		addr = addr[:j]
	}
	return Listen{Addr: addr, Port: port, Public: public(addr)}, true
}

// public: адрес доступен не только с самой машины и не только из частной сети.
func public(addr string) bool {
	switch addr {
	case "*", "0.0.0.0", "::":
		return true
	}
	ip := net.ParseIP(addr)
	if ip == nil {
		return false
	}
	return !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast()
}

// uniqListen: один порт на v4 и v6 - одна строка; публичность берётся самая широкая.
func uniqListen(list []Listen) []Listen {
	idx := map[int]int{}
	var out []Listen
	for _, l := range list {
		if i, ok := idx[l.Port]; ok {
			out[i].Public = out[i].Public || l.Public
			continue
		}
		idx[l.Port] = len(out)
		out = append(out, l)
	}
	return out
}

func uniq(list []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range list {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
