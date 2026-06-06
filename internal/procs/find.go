package procs

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/fuzzy"
)

// protected: без этих процессов система не живёт - убить их значит выйти из сеанса или уронить машину.
var protected = map[string]bool{
	"launchd": true, "kernel_task": true, "WindowServer": true, "loginwindow": true,
	"systemd": true, "init": true, "sshd": true, "login": true,
}

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

// Groups: процессы, сложенные по приложению или программе, по имени.
func Groups(list []Full) []App {
	groups := map[string]*App{}
	for _, p := range list {
		name := Name(p.Comm, p.Args)
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
	out := make([]App, 0, len(groups))
	for _, g := range groups {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Find: приложения, чьё имя подходит к запросу. Запрос сравнивается без пробелов и регистра
// и прощает пропущенные буквы: "goglehrome" найдёт Google Chrome.
func Find(list []Full, query string) []App {
	apps := Groups(list)
	keys := make([]string, len(apps))
	files := make([]string, len(apps))
	for i, a := range apps {
		keys[i] = strings.ReplaceAll(strings.ToLower(a.Name), " ", "")
		// Искать можно и по имени файла: человек помнит "qemu", а показываем "Android-эмулятор".
		files[i] = strings.ToLower(filepath.Base(a.Main.Comm))
	}
	q := strings.ReplaceAll(strings.ToLower(query), " ", "")
	// Точное имя - без выбора: kill launchd не должен спрашивать, какой из похожих.
	for i, k := range keys {
		if k == q {
			return []App{apps[i]}
		}
	}
	type hit struct {
		app   App
		rank  int
		order int
	}
	var hits []hit
	search := make([]string, len(keys))
	for i := range keys {
		search[i] = keys[i] + " " + files[i]
	}
	for i, m := range fuzzy.Filter(q, search) {
		// Слишком слабое совпадение - шум: на "go" подойдёт половина системы.
		if m.Score < len([]rune(q))*2 {
			continue
		}
		a := apps[m.Index]
		// Слово целиком в имени важнее совпадения по буквам, а приложение с окном - служебного
		// процесса: на "chrome" первым должен быть сам браузер, а не chrome-devtools-mcp.
		rank := 0
		if strings.Contains(search[m.Index], q) {
			rank += 2
		}
		if a.IsApp {
			rank++
		}
		hits = append(hits, hit{a, rank, i})
	}
	// Есть совпадение целым словом - разрозненные буквы уже не нужны: на "qemu" не надо
	// предлагать com.apple.quicklook, в котором q-e-m-u просто идут по порядку.
	whole := hits[:0:0]
	for _, h := range hits {
		if h.rank >= 2 {
			whole = append(whole, h)
		}
	}
	if len(whole) > 0 {
		hits = whole
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].rank != hits[j].rank {
			return hits[i].rank > hits[j].rank
		}
		return hits[i].order < hits[j].order
	})
	out := make([]App, len(hits))
	for i, h := range hits {
		out[i] = h.app
	}
	return out
}
