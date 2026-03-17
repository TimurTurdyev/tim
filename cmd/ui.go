package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/TimurTurdyev/tim/internal/brand"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/fuzzy"
	"github.com/TimurTurdyev/tim/internal/run"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/shellq"
	"github.com/TimurTurdyev/tim/internal/sshx"
	"github.com/TimurTurdyev/tim/internal/sysinfo"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
	"github.com/TimurTurdyev/tim/internal/visits"
)

// Экранный режим: tim без аргументов в терминале открывает главный экран. Данные для экранов
// собираются здесь, сами экраны - в internal/tui.

var uiCmd = &cobra.Command{
	Use:   "ui",
	Short: "Главный экран: поиск по проектам и командам, состояние машины",
	Args:  cobra.NoArgs,
	RunE:  func(cmd *cobra.Command, args []string) error { return openUI() },
}

func init() {
	root.AddCommand(uiCmd)
	// В терминале tim без аргументов - главный экран; в трубе и скриптах - заставка. Назначается
	// здесь, а не в описании root: иначе цикл инициализации root -> openUI -> root.
	root.RunE = func(cmd *cobra.Command, args []string) error { return openUI() }
}

func openUI() error {
	if !tui.Available() {
		overview()
		return nil
	}
	dir, err := tui.Run(tui.NewHome(homeOpts()), uiOptions())
	if errors.Is(err, tui.ErrNoTTY) {
		overview()
		return nil
	}
	if err != nil {
		return err
	}
	if dir != "" {
		// Шим читает папку из файла и делает cd; без шима подсказываем команду.
		if f := os.Getenv("TIM_CD_FILE"); f != "" {
			return os.WriteFile(f, []byte(dir), 0o600)
		}
		ui.Println(ui.Dim("перейти: cd ") + shellq.Quote(dir) + ui.Dim("  (tim init zsh - и tim сам сделает cd)"))
	}
	return nil
}

// exitCmds: команды, которым нужен терминал целиком - выполняются после закрытия экрана.
var exitCmds = map[string]bool{"ssh": true, "db": true, "tunnel": true, "open": true, "kill": true, "serve": true, "php": true,
	"hw": true, "disk": true, "check": true, "setup": true, "new": true, "sync": true, "each": true, "watch": true, "hook": true,
	"route": true, "cert": true, "link": true, "host": true, "init": true, "config": true, "self-update": true, "server": true}

// runLine: набранная в ":" команда. Экраны открываются экранами, остальное - результатом или после выхода.
func runLine(line string) tui.Action {
	args := strings.Fields(strings.TrimPrefix(strings.TrimSpace(line), "tim "))
	if len(args) == 0 {
		return tui.Action{}
	}
	switch args[0] {
	case "go", "proj":
		return tui.Action{Push: projectsScreen("")}
	case "ui":
		return tui.Action{}
	case "disk":
		if len(args) == 1 {
			if s, err := diskScreen(nil); err == nil {
				return tui.Action{Push: s}
			}
		}
		if len(args) == 2 && (args[1] == "-b" || args[1] == "--browse") {
			if st, err := newDiskState(nil); err == nil {
				return tui.Action{Push: browseScreen(st)}
			}
		}
		if len(args) == 2 && args[1] == "--leftovers" {
			return tui.Action{Push: leftScreen()}
		}
	}
	if exitCmds[args[0]] {
		return tui.Action{Exit: func() { runAfter(args) }}
	}
	return resultAction(args)
}

func resultAction(args []string) tui.Action {
	title := strings.Join(args, " ")
	return tui.Action{Push: tui.NewResult(title, func() (string, error) { return runInside(args) },
		tui.ResultOpts{Copy: copyText, Next: nextAfter(args)})}
}

// runInside выполняет команду tim, не выходя из экрана: вывод собирается, вопросы не задаются.
func runInside(args []string) (string, error) {
	prevIn, prevDis := runner.NoInput, tui.Disabled
	runner.NoInput, tui.Disabled = true, true
	defer func() { runner.NoInput, tui.Disabled = prevIn, prevDis }()
	resetFlags(args)
	var err error
	out := ui.Capture(func() {
		root.SetArgs(args)
		err = root.Execute()
	})
	root.SetArgs(nil)
	return out, err
}

// resetFlags: cobra помнит значения флагов между запусками в одном процессе; перед каждым - по умолчанию.
func resetFlags(args []string) {
	c, _, err := root.Find(args)
	if err != nil || c == nil {
		return
	}
	c.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Changed {
			_ = f.Value.Set(f.DefValue)
			f.Changed = false
		}
	})
}

func runAfter(args []string) {
	resetFlags(args)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		ui.Println(ui.Bad(translateError(err.Error())))
	}
}

func copyText(s string) {
	if runner.Has("pbcopy") {
		_, _ = runner.OutputStdin(s, "pbcopy")
	} else if runner.Has("xclip") {
		_, _ = runner.OutputStdin(s, "xclip", "-selection", "clipboard")
	}
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

// commandEntries: подсказки для командной строки - команды с описанием и примеры из справки.
func commandEntries(prefix string) []tui.CommandEntry {
	var out []tui.CommandEntry
	seen := map[string]bool{}
	var walk func(c *cobra.Command, path string)
	walk = func(c *cobra.Command, path string) {
		for _, sub := range c.Commands() {
			if sub.Hidden || sub.Name() == "help" || sub.Name() == "completion" || sub.Name() == "ui" {
				continue
			}
			line := strings.TrimSpace(path + " " + sub.Name())
			if !seen[line] {
				seen[line] = true
				out = append(out, tui.CommandEntry{Line: line, Desc: sub.Short})
			}
			walk(sub, line)
		}
	}
	walk(root, "")
	for _, ex := range examples {
		for _, l := range strings.Split(ex, "\n") {
			l = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), "tim "))
			desc := ""
			if i := strings.Index(l, "#"); i >= 0 {
				l, desc = strings.TrimSpace(l[:i]), strings.TrimSpace(l[i+1:])
			}
			if l != "" && !seen[l] {
				seen[l] = true
				out = append(out, tui.CommandEntry{Line: l, Desc: desc})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Line < out[j].Line })
	return out
}

// ---------- главный экран ----------

func homeOpts() tui.HomeOpts {
	return tui.HomeOpts{Load: loadHome, Search: searchHome}
}

func loadHome() tui.HomeData {
	host, _ := os.Hostname()
	host, _, _ = strings.Cut(host, ".")
	d := tui.HomeData{Status: host + " " + "\u00b7" + " " + time.Now().Format("15:04")}
	d.Recent = recentEntries()
	d.Sections = []tui.Section{
		{Key: "p", Title: "Проекты", Hint: "go, serve, db, check", Open: func() tui.Action { return tui.Action{Push: projectsScreen("")} }},
		{Key: "d", Title: "Диск", Hint: "обзор, папки, хвосты", Open: func() tui.Action {
			s, err := diskScreen(nil)
			if err != nil {
				return tui.Action{}
			}
			return tui.Action{Push: s}
		}},
		{Key: "m", Title: "Машина", Hint: "процессы, железо, службы", Open: func() tui.Action { return resultAction([]string{"hw"}) }},
		{Key: "n", Title: "Сеть и VPN", Hint: "обход, туннели, порты", Open: func() tui.Action { return resultAction([]string{"vpn"}) }},
		{Key: "w", Title: "Сайты", Hint: "домен, почта, сертификат", Open: func() tui.Action { return tui.Action{Exit: func() { runAfter([]string{"check"}) }} }},
		{Key: "u", Title: "Утилиты", Hint: "jwt, uuid, base64, cron", Open: func() tui.Action { return resultAction([]string{"help", "utils"}) }},
	}
	d.Tiles, d.Warning, d.WarnOpen = homeTiles()
	if on, depth := ui.Colors(); on {
		d.Art = brand.Raccoon(brand.Calm, depth)
	}
	return d
}

// homeTiles: плитки состояния и самое важное предупреждение.
func homeTiles() (tiles []tui.TileData, warning string, open func() tui.Action) {
	if disk, ok := sysinfo.DataDisk(); ok {
		pct := disk.Percent()
		tiles = append(tiles, tui.TileData{Title: "диск", Width: 22, Rows: []string{
			tui.Bar(float64(pct)/100, 10) + " " + pctText(pct),
			ui.Dim("свободно " + fmtBytes(int64(disk.Avail))),
		}})
		if pct >= 85 && warning == "" {
			warning = fmt.Sprintf("диск занят на %d%%, свободно %s", pct, fmtBytes(int64(disk.Avail)))
			open = func() tui.Action {
				s, err := diskScreen(nil)
				if err != nil {
					return tui.Action{}
				}
				return tui.Action{Push: s}
			}
		}
	}
	if used, total := sysinfo.Memory(); total > 0 {
		pct := int(used * 100 / total)
		tiles = append(tiles, tui.TileData{Title: "память", Width: 22, Rows: []string{
			tui.Bar(float64(pct)/100, 10) + " " + pctText(pct),
			ui.Dim(fmt.Sprintf("%.0f из %.0f GB", float64(used)/(1<<30), float64(total)/(1<<30))),
		}})
	}
	if len(cfg.Hosts) > 0 {
		phys, gw := physical()
		total, ok := 0, 0
		for _, h := range cfg.Hosts {
			for _, dest := range expandAddr(h.Addr) {
				total++
				if bypassed(dest, phys, gw) {
					ok++
				}
			}
		}
		guard := ui.Dim("страж не установлен")
		if st, found := guardStatus(); found && time.Since(st.LastCheck) < 10*time.Minute {
			guard = ui.Dim("страж работает")
		} else if warning == "" {
			warning = "страж маршрутов не установлен: после смены сети обход сломается"
			open = func() tui.Action { return resultAction([]string{"route", "status"}) }
		}
		lamp := ui.OK(fmt.Sprintf("\u25cf обход %d/%d", ok, total))
		if ok < total {
			lamp = ui.Warn(fmt.Sprintf("\u25b2 обход %d/%d", ok, total))
			if warning == "" {
				warning = fmt.Sprintf("%d целей идут через VPN вместо обхода", total-ok)
				open = func() tui.Action { return resultAction([]string{"vpn"}) }
			}
		}
		tiles = append(tiles, tui.TileData{Title: "vpn", Width: 24, Rows: []string{lamp, guard}})
	}
	var alive []run.State
	for _, s := range run.ListStates() {
		if s.Alive() {
			alive = append(alive, s)
		}
	}
	rows := []string{ui.Dim("\u25cb нет запущенных"), ui.Dim("tim serve <проект> -d")}
	if len(alive) > 0 {
		var names []string
		for _, s := range alive {
			names = append(names, s.Name+" :"+s.Port)
		}
		rows = []string{ui.OK(fmt.Sprintf("\u25cf %s", ui.Plural(len(alive), "работает", "работают", "работают"))),
			ui.Dim(strings.Join(names, ", "))}
	}
	tiles = append(tiles, tui.TileData{Title: "серверы", Width: 26, Rows: rows})
	return tiles, warning, open
}

func pctText(pct int) string {
	s := fmt.Sprintf("%d%%", pct)
	switch {
	case pct >= 85:
		return ui.Bad(s)
	case pct >= 60:
		return ui.Warn(s)
	}
	return s
}

// recentEntries: проекты, в которые заходили недавно.
func recentEntries() []tui.Hit {
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
		p := cfg.Projects[i]
		out = append(out, projectHit(&p, "проект, "+ago(e.Last)))
		if len(out) == 4 {
			break
		}
	}
	for i := range out {
		out[i].Kind = "недавнее"
	}
	return out
}

func projectHit(p *config.Project, hint string) tui.Hit {
	name := p.Name
	note := ""
	if st, ok := run.LoadState(name); ok && st.Alive() {
		note = ui.OK("\u25cf работает :" + st.Port)
	}
	return tui.Hit{Kind: "проекты", Title: name, Hint: hint, Note: note, Open: func() tui.Action {
		return tui.Action{Push: projectsScreen(name)}
	}}
}

// searchHome: проекты, команды, серверы и домены по запросу.
func searchHome(q string) []tui.Hit {
	var out []tui.Hit
	// Команды с примерами, самые похожие первыми.
	cmds := commandEntries("")
	lines := make([]string, len(cmds))
	for i, c := range cmds {
		lines[i] = c.Line + " " + c.Desc
	}
	for i, m := range fuzzy.Filter(q, lines) {
		if i == 4 {
			break
		}
		c := cmds[m.Index]
		args := strings.Fields(c.Line)
		out = append(out, tui.Hit{Kind: "команды", Title: c.Line, Hint: c.Desc, Open: func() tui.Action { return runLine(strings.Join(args, " ")) }})
	}
	names := make([]string, len(cfg.Projects))
	for i, p := range cfg.Projects {
		names[i] = p.Name
	}
	for i, m := range fuzzy.Filter(q, names) {
		if i == 5 {
			break
		}
		p := cfg.Projects[m.Index]
		out = append(out, projectHit(&p, tilde(p.Path)))
	}
	aliases := sshx.Aliases()
	for i, m := range fuzzy.Filter(q, aliases) {
		if i == 3 {
			break
		}
		a := aliases[m.Index]
		out = append(out, tui.Hit{Kind: "серверы", Title: a, Hint: "ssh мимо VPN", Open: func() tui.Action {
			return tui.Action{Exit: func() { runAfter([]string{"ssh", a}) }}
		}})
	}
	var domains []string
	for _, p := range cfg.Projects {
		if h, _ := hostPort(p.URL, ""); h != "" {
			domains = append(domains, h)
		}
	}
	for i, m := range fuzzy.Filter(q, domains) {
		if i == 3 {
			break
		}
		d := domains[m.Index]
		out = append(out, tui.Hit{Kind: "сайты", Title: d, Hint: "что за сайт: адреса, сертификат, почта", Open: func() tui.Action {
			return resultAction([]string{"site", d})
		}})
	}
	return out
}

// ---------- проекты ----------

func projectsScreen(focus string) tui.Screen {
	names := make([]string, len(cfg.Projects))
	for i, p := range cfg.Projects {
		names[i] = p.Name
	}
	order := visits.Order(names, visits.Load(), time.Now())
	rows := make([]tui.ProjectRow, 0, len(order))
	for _, i := range order {
		p := cfg.Projects[i]
		dir := cfg.ProjectDir(&p)
		row := tui.ProjectRow{Name: p.Name, Dir: dir, Stack: run.Stack(dir)}
		if st, ok := run.LoadState(p.Name); ok && st.Alive() {
			row.Running, row.Port = true, st.Port
		}
		if info, err := os.Stat(dir); err == nil {
			row.Age = agoShort(info.ModTime())
		}
		rows = append(rows, row)
	}
	if focus != "" {
		for i, r := range rows {
			if r.Name == focus && i > 0 {
				rows = append([]tui.ProjectRow{r}, append(rows[:i:i], rows[i+1:]...)...)
				break
			}
		}
	}
	return tui.NewProjects(tui.ProjectsOpts{
		Rows: rows,
		Card: projectCard,
		Go:   func(p tui.ProjectRow) tui.Action { visits.Record(p.Name, time.Now()); return tui.Action{Dir: p.Dir} },
		Actions: []tui.ProjectAction{
			{Key: "g", Label: "папка", Do: func(p tui.ProjectRow) tui.Action { visits.Record(p.Name, time.Now()); return tui.Action{Dir: p.Dir} }},
			{Key: "o", Label: "редактор", Do: func(p tui.ProjectRow) tui.Action {
				return tui.Action{Exit: func() { runAfter([]string{"open", p.Name}) }}
			}},
			{Key: "b", Label: "сайт", Do: func(p tui.ProjectRow) tui.Action { return resultAction([]string{"serve", "open", p.Name}) }},
			{Key: "s", Label: "сервер", Do: func(p tui.ProjectRow) tui.Action {
				if p.Running {
					return resultAction([]string{"serve", "stop", p.Name})
				}
				return tui.Action{Exit: func() { runAfter([]string{"serve", p.Name, "-d"}) }}
			}},
			{Key: "l", Label: "журнал", Do: func(p tui.ProjectRow) tui.Action { return resultAction([]string{"logs", p.Name}) }},
			{Key: "d", Label: "база", Do: func(p tui.ProjectRow) tui.Action {
				return tui.Action{Exit: func() { runAfter([]string{"db", p.Name}) }}
			}},
			{Key: "c", Label: "проверить", Do: func(p tui.ProjectRow) tui.Action { return resultAction([]string{"check", p.Name}) }},
			{Key: "e", Label: ".env", Do: func(p tui.ProjectRow) tui.Action { return resultAction([]string{"env", p.Name}) }},
		},
	})
}

// projectCard: карточка проекта - считается при выборе, git спрашивается здесь, а не для всех сразу.
func projectCard(row tui.ProjectRow) tui.ProjectCard {
	p, ok := cfg.Project(row.Name)
	if !ok {
		return tui.ProjectCard{}
	}
	label := func(s string) string { return ui.Dim(ui.Pad(s, 9)) }
	lines := []string{ui.Dim(tilde(rel(row.Dir))), ""}
	stack := row.Stack
	if v, err := runner.Output("php", "-r", "echo PHP_VERSION;"); err == nil && (stack == "laravel" || stack == "php") {
		stack += ", php " + strings.TrimSpace(v)
	}
	lines = append(lines, label("стек")+stack)
	if out, err := runner.Output("git", "-C", row.Dir, "status", "-sb"); err == nil {
		g := sysinfo.ParseGitStatusSB(out)
		lines = append(lines, label("ветка")+gitLine(g))
	}
	if row.Running {
		if st, ok := run.LoadState(row.Name); ok {
			lines = append(lines, label("сервер")+ui.OK("\u25cf работает :"+st.Port)+ui.Dim(", "+human(time.Since(st.Started))))
		}
	} else {
		lines = append(lines, label("сервер")+ui.Dim("\u25cb не запущен, порт "+projectPort(p, row.Dir)))
	}
	if p.URL != "" {
		lines = append(lines, label("сайт")+p.URL)
	}
	if p.DB != "" {
		lines = append(lines, label("база")+p.DB)
	}
	if p.SSH != "" {
		lines = append(lines, label("сервер")+p.SSH+ui.Dim("  "+p.Remote))
	}
	if p.Deploy != "" {
		lines = append(lines, label("деплой")+p.Deploy)
	}
	return tui.ProjectCard{Lines: lines}
}

// agoShort: давность одним числом - для колонок списка.
func agoShort(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%d мин", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d ч", int(d.Hours()))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%d дн", int(d.Hours()/24))
	}
	return fmt.Sprintf("%d мес", int(d.Hours()/24/30))
}

// rel: путь от домашней папки для показа.
func rel(p string) string {
	home, _ := os.UserHomeDir()
	if r, err := filepath.Rel(home, p); err == nil && !strings.HasPrefix(r, "..") {
		return r
	}
	return p
}
