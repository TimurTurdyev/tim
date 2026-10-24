package home

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/bypass"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/disk"
	"github.com/TimurTurdyev/tim/internal/fuzzy"
	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/machine"
	"github.com/TimurTurdyev/tim/internal/motd"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/projects"
	"github.com/TimurTurdyev/tim/internal/run"
	"github.com/TimurTurdyev/tim/internal/servers"
	"github.com/TimurTurdyev/tim/internal/sites"
	"github.com/TimurTurdyev/tim/internal/sshx"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
	"github.com/TimurTurdyev/tim/internal/visits"
)

// recentRows: сколько недавнего держать в списке; сколько показать, решает экран по своей высоте.
const recentRows = 15

// layoutKeys: буквы разделов в русской раскладке.
var layoutKeys = map[string]string{"з": "p", "в": "d", "ь": "m", "т": "n", "ы": "s", "ц": "w", "г": "u"}

// exitCmds: команды, которым нужен терминал целиком - выполняются после закрытия экрана.
var exitCmds = map[string]bool{"ssh": true, "db": true, "tunnel": true, "open": true, "serve": true, "php": true, "direct": true,
	"disk": true, "setup": true, "new": true, "sync": true, "each": true, "watch": true, "hook": true,
	"route": true, "cert": true, "link": true, "host": true, "init": true, "config": true, "self-update": true}

// changingWords: слова, после которых команда меняет систему.
var changingWords = map[string]bool{"kill": true, "rm": true, "stop": true, "fix": true, "add": true,
	"--clean": true, "clean": true, "-y": true, "--yes": true}

func (h *Home) screen() tui.Screen {
	index := sync.OnceValue(h.newIndex)
	return tui.NewHome(tui.HomeOpts{Load: h.load, Machine: h.machine, Search: func(q string) []tui.Hit { return h.search(index(), q) }})
}

// runLine: набранная в ":" команда. Экраны открываются экранами, остальное - результатом или после выхода.
func (h *Home) runLine(line string) tui.Action {
	args := strings.Fields(strings.TrimPrefix(strings.TrimSpace(line), "tim "))
	if len(args) == 0 || args[0] == "ui" {
		return tui.Action{}
	}
	if s := h.screenFor(args); s != nil {
		return tui.Action{Push: s}
	}
	if exitCmds[args[0]] {
		return tui.Action{Exit: func() { h.after(args) }}
	}
	return h.result(args)
}

// screenFor: у команды есть свой экран - он и открывается; nil - экрана нет.
func (h *Home) screenFor(args []string) tui.Screen {
	env, one := h.env, len(args) == 1
	switch {
	case args[0] == "go" || args[0] == "proj":
		return h.projectsScreen("", app.TabAll)
	case args[0] == "hw":
		return machine.HWScreen()
	case args[0] == "vpn" && one:
		return bypass.Screen(env)
	case args[0] == "check" && one:
		return sites.CheckScreen(env)
	case args[0] == "serve" && len(args) == 2 && args[1] == "list":
		return h.projectsScreen("", app.TabRunning)
	case args[0] == "domain" && len(args) == 2:
		if d, err := sites.DomainOf(env, args[1]); err == nil {
			return sites.DomainScreen(env, d)
		}
	case args[0] == "server" && len(args) == 2:
		return servers.Screen(env, servers.Target(env.Cfg, args[1]))
	case args[0] == "servers" && one:
		return servers.ServersScreen(env, servers.Targets(env, nil, false))
	case args[0] == "top" && one:
		return machine.ProcsScreen("")
	case (args[0] == "ps" || args[0] == "kill") && len(args) == 2:
		return machine.ProcsScreen(args[1])
	case args[0] == "disk":
		return diskScreen(env, args[1:])
	}
	return nil
}

// projectsScreen: проекты вкладками - Все, Запущенные, Проверка; focus - кого показать первым.
func (h *Home) projectsScreen(focus string, tab int) tui.Screen {
	env := h.env
	return tui.NewTabs([]string{"Проекты"}, []tui.Tab{
		projects.AllTab(env, focus, func() map[string]string { return machine.RunningPorts(env.Cfg) }),
		machine.RunningTab(env),
		sites.CheckTab(env),
	}, tab)
}

func diskScreen(env *app.Env, flags []string) tui.Screen {
	var s tui.Screen
	var err error
	switch {
	case len(flags) == 0:
		s, err = disk.Screen(env, nil, false)
	case len(flags) == 1 && (flags[0] == "-b" || flags[0] == "--browse"):
		s, err = disk.BrowseScreen(env)
	case len(flags) == 1 && flags[0] == "--leftovers":
		s = disk.LeftoversScreen(env.Cfg)
	}
	if err != nil {
		return nil
	}
	return s
}

// nextAfter: что логично сделать после команды - подсказки цифрами на экране результата.
func nextAfter(args []string) []tui.CommandEntry {
	if len(args) < 2 {
		return nil
	}
	arg := args[1]
	switch args[0] {
	case "cert", "site", "mail", "dns", "http":
		return []tui.CommandEntry{{Line: "domain " + arg}, {Line: "mail " + arg}, {Line: "http " + arg}}
	case "port":
		return []tui.CommandEntry{{Line: "ports"}, {Line: "ports --old"}}
	}
	return nil
}

func (h *Home) load() tui.HomeData {
	return tui.HomeData{
		Header:   motd.Lockup(h.env.Cfg, h.env.Build(), false),
		Recent:   h.recent(),
		Sections: h.sections(),
	}
}

// machine: беды, строки машины и бейджи разделов - то, что считается дольше остального.
func (h *Home) machine() tui.HomeMachine {
	rows, m := motd.Rows(h.env.Cfg, nil)
	out := tui.HomeMachine{Mood: m.Mood(time.Now()), Badges: map[string]string{}}
	counts := map[string][2]int{}
	add := func(lvl level.Level, text, dest, key string, open func() tui.Action) {
		title := ui.Warn("\u25b2 " + text)
		c := counts[key]
		if lvl == level.Bad {
			title = ui.Bad("\u2715 " + text)
			c[0]++
		} else {
			c[1]++
		}
		counts[key] = c
		out.Attention = append(out.Attention, tui.Hit{Kind: tui.KindAttention, Title: title, Hint: dest, Open: open})
	}
	// Порядок: память и диск машины, сайты, обход и страж, серверы.
	sitesOpen := func() tui.Action { return tui.Action{Push: sites.SitesScreen(h.env)} }
	sitesDone := false
	for _, w := range m.Warnings() {
		if (w.Kind == health.Bypass || w.Kind == health.Guard) && !sitesDone {
			for _, a := range sites.Attention(h.env.Cfg) {
				add(a.Level, a.Text, "Сайты", "w", sitesOpen)
			}
			sitesDone = true
		}
		dest, key, open := h.trouble(w, m.MemHolder)
		add(level.Warn, w.Text, dest, key, open)
	}
	if !sitesDone {
		for _, a := range sites.Attention(h.env.Cfg) {
			add(a.Level, a.Text, "Сайты", "w", sitesOpen)
		}
	}
	for _, a := range servers.Attention(h.env) {
		target, _, _ := strings.Cut(a.Text, ":")
		add(a.Level, a.Text, "Серверы", "s", func() tui.Action {
			return tui.Action{Push: servers.Screen(h.env, target)}
		})
	}
	// У Сайтов бейдж - число сайтов с бедой, а не строк ВНИМАНИЯ.
	if bad, warn := sites.Troubled(h.env.Cfg); bad+warn > 0 {
		counts["w"] = [2]int{bad, warn}
	}
	for key, c := range counts {
		out.Badges[key] = badge(c[0], c[1])
	}
	for _, r := range rows {
		if r.Label == "" {
			continue
		}
		dest, open := h.lineDest(r.Dest)
		out.Machine = append(out.Machine, tui.Hit{Kind: tui.KindMachine, Title: r.Label, Note: r.Value, Hint: dest, Open: open})
	}
	return out
}

// badge: число бед раздела лампами, сначала ошибки, потом предупреждения.
func badge(bad, warn int) string {
	var parts []string
	if bad > 0 {
		parts = append(parts, ui.Bad("\u2715 "+strconv.Itoa(bad)))
	}
	if warn > 0 {
		parts = append(parts, ui.Warn("\u25b2 "+strconv.Itoa(warn)))
	}
	return strings.Join(parts, " ")
}

// trouble: куда ведёт беда машины - подпись, буква раздела для бейджа и переход.
func (h *Home) trouble(w health.Warning, holder string) (string, string, func() tui.Action) {
	switch w.Kind {
	case health.DiskFull:
		return "Диск", "d", func() tui.Action { return tui.Action{Push: diskScreen(h.env, nil)} }
	case health.Memory:
		return "Процессы", "m", func() tui.Action { return tui.Action{Push: machine.MemoryScreen(w.Full(), holder)} }
	}
	return "Сеть", "n", func() tui.Action { return tui.Action{Push: bypass.Screen(h.env)} }
}

// lineDest: куда ведёт строка машины.
func (h *Home) lineDest(d motd.Dest) (string, func() tui.Action) {
	switch d {
	case motd.ToProcs:
		return "Процессы по памяти", func() tui.Action { return tui.Action{Push: machine.MemoryScreen("", "")} }
	case motd.ToDisk:
		return "Диск", func() tui.Action { return tui.Action{Push: diskScreen(h.env, nil)} }
	case motd.ToNet:
		return "Сеть", func() tui.Action { return tui.Action{Push: bypass.Screen(h.env)} }
	case motd.ToRunning:
		return "Проекты " + "\u203a" + " Запущенные", func() tui.Action { return tui.Action{Push: h.projectsScreen("", app.TabRunning)} }
	}
	return "", nil
}

// sections: разделы главного экрана; по ним же ищет строка поиска.
func (h *Home) sections() []tui.Section {
	push := func(screen func() tui.Screen) func() tui.Action {
		return func() tui.Action { return tui.Action{Push: screen()} }
	}
	return []tui.Section{
		{Key: "p", Title: "Проекты", Hint: "go, serve, запущенные, проверка", Open: push(func() tui.Screen { return h.projectsScreen("", app.TabAll) })},
		{Key: "s", Title: "Серверы", Hint: "состояние, кто ломится, ssh", Open: push(func() tui.Screen {
			return servers.ServersScreen(h.env, servers.Targets(h.env, nil, false))
		})},
		{Key: "w", Title: "Сайты", Hint: "ответ, сертификат, почта, домен", Open: push(func() tui.Screen { return sites.SitesScreen(h.env) })},
		{Key: "n", Title: "Сеть", Hint: "обход VPN, страж, туннели", Open: push(func() tui.Screen { return bypass.Screen(h.env) })},
		{Key: "m", Title: "Машина", Hint: "железо, процессы", Open: push(machine.HWScreen)},
		{Key: "d", Title: "Диск", Hint: "обзор, папки, хвосты", Open: push(func() tui.Screen { return diskScreen(h.env, nil) })},
		{Key: "u", Title: "Утилиты", Hint: "jwt, uuid, base64, cron", Open: push(h.utilsScreen)},
	}
}

// recent: проекты, куда заходил, и команды, которые набирал, - вперемешку по времени. Команда по enter
// встаёт в строку ":", а не выполняется: среди них бывают и меняющие систему.
func (h *Home) recent() []tui.Hit {
	type item struct {
		hit  tui.Hit
		last time.Time
	}
	cfg := h.env.Cfg
	var items []item
	v := visits.Load()
	for i := range cfg.Projects {
		p := &cfg.Projects[i]
		if e, ok := v[p.Name]; ok {
			hit := h.projectHit(p, age(e.Last)+" \u00b7 проект")
			items = append(items, item{hit, e.Last})
		}
	}
	for line, e := range visits.Commands() {
		hit := tui.Hit{Title: line, Hint: age(e.Last), Open: func() tui.Action { return h.runLine(line) }}
		// Меняющая команда не выполняется с одного enter: она встаёт в ":" для правки.
		if changing(line) {
			hit.Edit = true
			hit.Open = func() tui.Action { return tui.Action{Command: line} }
		}
		items = append(items, item{hit, e.Last})
	}
	slices.SortStableFunc(items, func(a, b item) int {
		return cmp.Or(b.last.Compare(a.last), cmp.Compare(a.hit.Title, b.hit.Title))
	})
	var out []tui.Hit
	for _, it := range items[:min(recentRows, len(items))] {
		it.hit.Kind = tui.KindRecent
		out = append(out, it.hit)
	}
	return out
}

// age: давность одной единицей и без "назад" - "5 ч", "2 дн", "только что".
func age(t time.Time) string {
	if time.Since(t) < time.Minute {
		return "только что"
	}
	return ui.Short(time.Since(t))
}

// changing: команда меняет систему или забирает терминал - её не выполнять с одного enter.
func changing(line string) bool {
	f := strings.Fields(line)
	if len(f) == 0 {
		return false
	}
	if exitCmds[f[0]] {
		return true
	}
	for _, w := range f[1:] {
		if changingWords[w] {
			return true
		}
	}
	return false
}

func (h *Home) projectHit(p *config.Project, hint string) tui.Hit {
	name := p.Name
	note := ""
	if st, ok := run.LoadState(name); ok && st.Alive() {
		note = ui.OK("\u25cf :" + st.Port)
	}
	return tui.Hit{Kind: "проекты", Title: name, Hint: hint, Note: note, Open: func() tui.Action {
		return tui.Action{Push: h.projectsScreen(name, app.TabAll)}
	}}
}

// searchIndex: то, по чему ищет главный экран; собирается раз на экран, а не на каждую букву.
type searchIndex struct {
	cmds     []tui.CommandEntry
	cmdNames []string
	lines    []string // команда, её описание и слова
	names    []string
	aliases  []string
	domains  []string
	sections []tui.Section
	secLines []string // название раздела и подсказка
}

func (h *Home) newIndex() *searchIndex {
	cfg := h.env.Cfg
	ix := &searchIndex{cmds: h.commands(""), aliases: sshx.Aliases(), domains: sites.Domains(cfg)}
	for _, c := range ix.cmds {
		ix.cmdNames = append(ix.cmdNames, c.Line)
		ix.lines = append(ix.lines, searchLine(c.Line, c.Desc))
	}
	for _, p := range cfg.Projects {
		ix.names = append(ix.names, p.Name)
	}
	ix.sections = h.sections()
	for _, s := range ix.sections {
		ix.secLines = append(ix.secLines, s.Title+" "+s.Hint)
	}
	return ix
}

// hitGroup: находки одного вида; группа с более точным совпадением идёт выше. Очки fuzzy между
// группами не сравнить: у команд ищется по строке с описанием.
type hitGroup struct {
	hits  []tui.Hit
	exact int // 3 - имя целиком, 2 - с начала, 1 - подстрокой, 0 - по буквам
}

func (g *hitGroup) add(hit tui.Hit, q string) {
	g.hits = append(g.hits, hit)
	g.exact = max(g.exact, exactness(q, hit.Title))
}

// sectionKey: запрос из одной буквы как клавиша раздела, с русской раскладкой.
func sectionKey(q string) string {
	q = strings.ToLower(strings.TrimSpace(q))
	if k, ok := layoutKeys[q]; ok {
		return k
	}
	return q
}

func exactness(q, title string) int {
	q, title = strings.ToLower(strings.TrimSpace(q)), strings.ToLower(title)
	switch {
	case title == q:
		return 3
	case strings.HasPrefix(title, q):
		return 2
	case strings.Contains(title, q):
		return 1
	}
	return 0
}

// search: команды, проекты, серверы и домены по запросу; внутри группы - самые похожие первыми.
func (h *Home) search(ix *searchIndex, q string) []tui.Hit {
	cfg := h.env.Cfg
	groups := make([]hitGroup, 5)
	for _, i := range rankCommands(q, ix.cmdNames, ix.lines, 4) {
		c := ix.cmds[i]
		hit := tui.Hit{Kind: "команды", Title: c.Line, Hint: c.Desc, Open: func() tui.Action { return h.runLine(c.Line) }}
		if c.Template {
			// Пример с выдуманными значениями: enter подставляет его в ":" для правки.
			hit.Note = ui.Dim("пример")
			hit.Open = func() tui.Action { return tui.Action{Command: c.Line} }
		}
		groups[0].add(hit, q)
	}
	for _, m := range fuzzy.FilterTop(q, ix.names, 5) {
		p := &cfg.Projects[m.Index]
		groups[1].add(h.projectHit(p, paths.Show(cfg.ProjectDir(p))), q)
	}
	for _, m := range fuzzy.FilterTop(q, ix.aliases, 3) {
		a := ix.aliases[m.Index]
		groups[2].add(tui.Hit{Kind: "серверы", Title: a, Hint: "ssh мимо VPN", Open: func() tui.Action {
			return tui.Action{Exit: func() { h.after([]string{"ssh", a}) }}
		}}, q)
	}
	for _, m := range fuzzy.FilterTop(q, ix.domains, 3) {
		d := ix.domains[m.Index]
		groups[3].add(tui.Hit{Kind: "сайты", Title: d, Hint: "домен: имена, адреса, сертификат, почта", Open: func() tui.Action {
			return h.runLine("domain " + d)
		}}, q)
	}
	secHit := func(sec tui.Section) tui.Hit {
		return tui.Hit{Kind: "разделы", Title: sec.Title, Hint: sec.Hint, Note: ui.Dim(sec.Key), Open: sec.Open}
	}
	byKey := ""
	for _, sec := range ix.sections {
		if sectionKey(q) == sec.Key {
			byKey = sec.Key
			groups[4].hits = append(groups[4].hits, secHit(sec))
			groups[4].exact = 4 // буква раздела: раздел первым, enter его открывает
		}
	}
	for _, m := range fuzzy.FilterTop(q, ix.secLines, 3) {
		if sec := ix.sections[m.Index]; sec.Key != byKey {
			groups[4].add(secHit(sec), q)
		}
	}
	slices.SortStableFunc(groups, func(a, b hitGroup) int { return cmp.Compare(b.exact, a.exact) })
	var out []tui.Hit
	for _, g := range groups {
		out = append(out, g.hits...)
	}
	return out
}
