package procs

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/fuzzy"
)

// Full: процесс со всем, что нужно, чтобы решить, как его закрыть.
type Full struct {
	Sample
	PPID   string
	UID    int
	Uptime time.Duration
	State  string
}

// ParseFull разбирает ps -Axo pid=,ppid=,uid=,rss=,pcpu=,etime=,state=,comm=.
func ParseFull(out string) []Full {
	var list []Full
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 8 {
			continue
		}
		uid, err1 := strconv.Atoi(f[2])
		kb, err2 := strconv.ParseInt(f[3], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		cpu, _ := strconv.ParseFloat(strings.Replace(f[4], ",", ".", 1), 64)
		up, _ := ParseEtime(f[5])
		list = append(list, Full{
			Sample: Sample{PID: f[0], RSS: kb * 1024, CPU: cpu, Comm: strings.Join(f[7:], " ")},
			PPID:   f[1], UID: uid, Uptime: up, State: f[6],
		})
	}
	return list
}

// protected: без этих процессов система не живёт - убить их значит выйти из сеанса или уронить машину.
var protected = map[string]bool{
	"launchd": true, "kernel_task": true, "WindowServer": true, "loginwindow": true,
	"systemd": true, "init": true, "sshd": true, "login": true,
}

// Protected: процесс трогать нельзя.
func Protected(p Full) bool {
	return p.PID == "0" || p.PID == "1" || protected[AppName(p.Comm)]
}

// App: процессы одного приложения или одной программы.
type App struct {
	Name  string
	Procs []Full
	RSS   int64
	CPU   float64
	Main  Full // самый старший: у приложения это главный процесс, у остальных - первый по времени
	IsApp bool // из .app - его можно закрыть как Cmd+Q
}

// Find: приложения, чьё имя подходит к запросу. Запрос сравнивается без пробелов и регистра
// и прощает пропущенные буквы: "goglehrome" найдёт Google Chrome.
func Find(list []Full, query string) []App {
	groups := map[string]*App{}
	for _, p := range list {
		name := AppName(p.Comm)
		g := groups[name]
		if g == nil {
			g = &App{Name: name, IsApp: strings.Contains(p.Comm, ".app/")}
			groups[name] = g
		}
		g.Procs = append(g.Procs, p)
		g.RSS += p.RSS
		g.CPU += p.CPU
		if g.Main.PID == "" || p.Uptime > g.Main.Uptime {
			g.Main = p
		}
	}
	names := make([]string, 0, len(groups))
	for n := range groups {
		names = append(names, n)
	}
	sort.Strings(names)
	keys := make([]string, len(names))
	for i, n := range names {
		keys[i] = strings.ReplaceAll(strings.ToLower(n), " ", "")
	}
	q := strings.ReplaceAll(strings.ToLower(query), " ", "")
	// Точное имя - без выбора: kill launchd не должен спрашивать, какой из похожих.
	for i, k := range keys {
		if k == q {
			return []App{*groups[names[i]]}
		}
	}
	var out []App
	for _, m := range fuzzy.Filter(q, keys) {
		// Слишком слабое совпадение - шум: на "go" подойдёт половина системы.
		if m.Score < len([]rune(q))*2 {
			continue
		}
		out = append(out, *groups[names[m.Index]])
	}
	return out
}
