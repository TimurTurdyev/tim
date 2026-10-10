package home

import (
	"cmp"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/brand"
	"github.com/TimurTurdyev/tim/internal/bypass"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/disk"
	"github.com/TimurTurdyev/tim/internal/fuzzy"
	"github.com/TimurTurdyev/tim/internal/machine"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/projects"
	"github.com/TimurTurdyev/tim/internal/run"
	"github.com/TimurTurdyev/tim/internal/serve"
	"github.com/TimurTurdyev/tim/internal/servers"
	"github.com/TimurTurdyev/tim/internal/sites"
	"github.com/TimurTurdyev/tim/internal/sshx"
	"github.com/TimurTurdyev/tim/internal/sysinfo"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
	"github.com/TimurTurdyev/tim/internal/visits"
)

// exitCmds: команды, которым нужен терминал целиком - выполняются после закрытия экрана.
var exitCmds = map[string]bool{"ssh": true, "db": true, "tunnel": true, "open": true, "serve": true, "php": true, "direct": true,
	"disk": true, "setup": true, "new": true, "sync": true, "each": true, "watch": true, "hook": true,
	"route": true, "cert": true, "link": true, "host": true, "init": true, "config": true, "self-update": true}

func (h *Home) screen() tui.Screen {
	index := sync.OnceValue(h.newIndex)
	return tui.NewHome(tui.HomeOpts{Load: h.load, Search: func(q string) []tui.Hit { return h.search(index(), q) }})
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
		return projects.Screen(env, "")
	case args[0] == "hw":
		return machine.HWScreen()
	case args[0] == "vpn" && one:
		return bypass.Screen(env)
	case args[0] == "check" && one:
		return sites.CheckScreen(env)
	case args[0] == "serve" && len(args) == 2 && args[1] == "list":
		return serve.Screen(env)
	case args[0] == "domain" && len(args) == 2:
		if d, err := sites.DomainOf(env, args[1]); err == nil {
			return sites.DomainScreen(env, d)
		}
	case args[0] == "server" && len(args) == 2:
		return servers.Screen(env, servers.Target(env.Cfg, args[1]))
	case args[0] == "top" && one:
		return machine.ProcsScreen("")
	case (args[0] == "ps" || args[0] == "kill") && len(args) == 2:
		return machine.ProcsScreen(args[1])
	case args[0] == "disk":
		return diskScreen(env, args[1:])
	}
	return nil
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
	host, _ := os.Hostname()
	host, _, _ = strings.Cut(host, ".")
	d := tui.HomeData{Status: host + " \u00b7 " + time.Now().Format("15:04")}
	d.Recent = h.recent()
	push := func(screen func() tui.Screen) func() tui.Action {
		return func() tui.Action { return tui.Action{Push: screen()} }
	}
	d.Sections = []tui.Section{
		{Key: "p", Title: "Проекты", Hint: "go, serve, db, check", Open: push(func() tui.Screen { return projects.Screen(h.env, "") })},
		{Key: "d", Title: "Диск", Hint: "обзор, папки, хвосты", Open: push(func() tui.Screen { return diskScreen(h.env, nil) })},
		{Key: "m", Title: "Машина", Hint: "железо, процессы", Open: push(machine.HWScreen)},
		{Key: "n", Title: "Сеть и VPN", Hint: "обход, страж, туннели", Open: push(func() tui.Screen { return bypass.Screen(h.env) })},
		{Key: "s", Title: "Dev-серверы", Hint: "фоновые, журналы", Open: push(func() tui.Screen { return serve.Screen(h.env) })},
		{Key: "w", Title: "Сайты", Hint: "проверка, домен, почта", Open: push(func() tui.Screen { return sites.CheckScreen(h.env) })},
		{Key: "u", Title: "Утилиты", Hint: "jwt, uuid, base64, cron", Open: func() tui.Action { return h.result([]string{"help", "utils"}) }},
	}
	d.Tiles, d.Warning, d.WarnOpen = h.tiles()
	if on, depth := ui.Colors(); on {
		d.Art = brand.Mascot(brand.Calm, depth)
	}
	return d
}

// tiles: плитки состояния и самое важное предупреждение - что открыть по нему.
func (h *Home) tiles() (tiles []tui.TileData, warning string, open func() tui.Action) {
	warn := func(text string, screen func() tui.Screen) {
		if warning == "" {
			warning, open = text, func() tui.Action { return tui.Action{Push: screen()} }
		}
	}
	if data, ok := sysinfo.DataDisk(); ok {
		pct := data.Percent()
		tiles = append(tiles, tui.TileData{Title: "диск", Width: 22, Rows: []string{
			tui.Bar(float64(pct)/100, 10) + " " + ui.Percent(pct),
			ui.Dim("свободно " + ui.Bytes(int64(data.Avail))),
		}})
		if pct >= 85 {
			warn(fmt.Sprintf("диск занят на %d%%, свободно %s", pct, ui.Bytes(int64(data.Avail))), func() tui.Screen { return diskScreen(h.env, nil) })
		}
	}
	if used, total := sysinfo.Memory(); total > 0 {
		pct := int(used * 100 / total)
		tiles = append(tiles, tui.TileData{Title: "память", Width: 22, Rows: []string{
			tui.Bar(float64(pct)/100, 10) + " " + ui.Percent(pct),
			ui.Dim(fmt.Sprintf("%.0f из %.0f GB", float64(used)/(1<<30), float64(total)/(1<<30))),
		}})
	}
	if len(h.env.Cfg.Hosts) > 0 {
		tiles = append(tiles, h.vpnTile(warn))
	}
	return append(tiles, serversTile()), warning, open
}

func (h *Home) vpnTile(warn func(string, func() tui.Screen)) tui.TileData {
	vpn := func() tui.Screen { return bypass.Screen(h.env) }
	ok, total := bypass.BypassCount(h.env.Cfg)
	guard := ui.Dim("страж не установлен")
	if st, found := bypass.GuardStatus(); found && time.Since(st.LastCheck) < 10*time.Minute {
		guard = ui.Dim("страж работает")
	} else {
		warn("страж маршрутов не установлен: после смены сети обход сломается", vpn)
	}
	lamp := ui.OK(fmt.Sprintf("\u25cf обход %d/%d", ok, total))
	if ok < total {
		lamp = ui.Warn(fmt.Sprintf("\u25b2 обход %d/%d", ok, total))
		warn(fmt.Sprintf("%d целей идут через VPN вместо обхода", total-ok), vpn)
	}
	return tui.TileData{Title: "vpn", Width: 24, Rows: []string{lamp, guard}}
}

func serversTile() tui.TileData {
	var names []string
	for _, s := range run.ListStates() {
		if s.Alive() {
			names = append(names, s.Name+" :"+s.Port)
		}
	}
	rows := []string{ui.Dim("\u25cb нет запущенных"), ui.Dim("tim serve <проект> -d")}
	if len(names) > 0 {
		rows = []string{ui.OK("\u25cf " + ui.Plural(len(names), "работает", "работают", "работают")), ui.Dim(strings.Join(names, ", "))}
	}
	return tui.TileData{Title: "серверы", Width: 26, Rows: rows}
}

// recent: проекты, в которые заходили недавно.
func (h *Home) recent() []tui.Hit {
	cfg := h.env.Cfg
	names := make([]string, len(cfg.Projects))
	for i, p := range cfg.Projects {
		names[i] = p.Name
	}
	v := visits.Load()
	var out []tui.Hit
	for _, i := range visits.Order(names, v, time.Now()) {
		e, ok := v[names[i]]
		if !ok {
			continue
		}
		hit := h.projectHit(&cfg.Projects[i], "проект, "+ui.Ago(e.Last))
		hit.Kind = "недавнее"
		if out = append(out, hit); len(out) == 4 {
			break
		}
	}
	return out
}

func (h *Home) projectHit(p *config.Project, hint string) tui.Hit {
	name := p.Name
	note := ""
	if st, ok := run.LoadState(name); ok && st.Alive() {
		note = ui.OK("\u25cf работает :" + st.Port)
	}
	return tui.Hit{Kind: "проекты", Title: name, Hint: hint, Note: note, Open: func() tui.Action {
		return tui.Action{Push: projects.Screen(h.env, name)}
	}}
}

// searchIndex: то, по чему ищет главный экран; собирается раз на экран, а не на каждую букву.
type searchIndex struct {
	cmds    []tui.CommandEntry
	lines   []string // команда и её описание
	names   []string
	aliases []string
	domains []string
}

func (h *Home) newIndex() *searchIndex {
	cfg := h.env.Cfg
	ix := &searchIndex{cmds: h.commands(""), aliases: sshx.Aliases(), domains: sites.Domains(cfg)}
	for _, c := range ix.cmds {
		ix.lines = append(ix.lines, c.Line+" "+c.Desc)
	}
	for _, p := range cfg.Projects {
		ix.names = append(ix.names, p.Name)
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
	groups := make([]hitGroup, 4)
	for _, m := range fuzzy.FilterTop(q, ix.lines, 4) {
		c := ix.cmds[m.Index]
		groups[0].add(tui.Hit{Kind: "команды", Title: c.Line, Hint: c.Desc, Open: func() tui.Action { return h.runLine(c.Line) }}, q)
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
		groups[3].add(tui.Hit{Kind: "сайты", Title: d, Hint: "что за сайт: адреса, сертификат, почта", Open: func() tui.Action {
			return h.result([]string{"site", d})
		}}, q)
	}
	slices.SortStableFunc(groups, func(a, b hitGroup) int { return cmp.Compare(b.exact, a.exact) })
	var out []tui.Hit
	for _, g := range groups {
		out = append(out, g.hits...)
	}
	return out
}
