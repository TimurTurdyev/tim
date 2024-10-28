package cmd

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/envfile"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/phpver"
	"github.com/TimurTurdyev/tim/internal/registry"
	"github.com/TimurTurdyev/tim/internal/run"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// projectFor: по имени, по текущей папке, а при tty - выбором из реестра.
func projectFor(name string) (*config.Project, error) {
	p, err := registry.Pick(cfg, name)
	if err != nil && name == "" && len(cfg.Projects) > 0 && tui.Available() {
		return pickProject()
	}
	return p, err
}

func projectPort(p *config.Project, dir string) string {
	if p.Port > 0 {
		return strconv.Itoa(p.Port)
	}
	if u, ok := envfile.Get(filepath.Join(dir, ".env"), "APP_URL"); ok {
		if port := envfile.Port(u); port != "" {
			return port
		}
	}
	return "8000"
}

// resolveProjects: имена, группы @имя и части имён; пусто - проект текущей папки или пикер.
func resolveProjects(args []string) ([]*config.Project, error) {
	if len(args) == 0 {
		p, err := projectFor("")
		if err != nil {
			return nil, err
		}
		return []*config.Project{p}, nil
	}
	names := make([]string, len(cfg.Projects))
	for i, p := range cfg.Projects {
		names[i] = p.Name
	}
	var out []*config.Project
	for _, a := range args {
		if g, ok := strings.CutPrefix(a, "@"); ok {
			members, ok := cfg.Groups[g]
			if !ok {
				return nil, ui.Errorf("нет группы %s в [groups] конфига", g)
			}
			for _, m := range members {
				p, ok := cfg.Project(m)
				if !ok {
					return nil, ui.Errorf("в группе %s нет проекта %s в реестре", g, m)
				}
				out = append(out, p)
			}
			continue
		}
		idx := run.MatchNames(names, a)
		switch {
		case len(idx) == 1:
			out = append(out, &cfg.Projects[idx[0]])
		case len(idx) == 0:
			return nil, ui.Errorf("нет проекта %s: tim proj list", a)
		case tui.Available():
			items := make([]tui.Item, len(idx))
			for k, i := range idx {
				items[k] = tui.Item{Title: names[i], Hint: cfg.Projects[i].Path}
			}
			k, err := tui.Pick(a, items)
			if err != nil {
				return nil, pickErr(err, "проект", items)
			}
			out = append(out, &cfg.Projects[idx[k]])
		default:
			var list []string
			for _, i := range idx {
				list = append(list, names[i])
			}
			return nil, ui.Errorf("%s подходит к нескольким: %s", a, strings.Join(list, " "))
		}
	}
	return out, nil
}

// phpEnv: если активный php не подходит под require.php проекта, PATH с подходящей версией из brew.
func phpEnv(dir, stack string) ([]string, string) {
	if stack != "laravel" && stack != "php" {
		return nil, ""
	}
	req := phpver.Required(filepath.Join(dir, "composer.json"))
	if req == "" || !runner.Has("brew") {
		return nil, ""
	}
	if active, err := runner.Output("php", "-r", "echo PHP_VERSION;"); err == nil {
		if v, ok := phpver.Parse(active); ok && phpver.Satisfies(v, req) {
			return nil, ""
		}
	}
	for _, i := range phpInstalls() {
		if phpver.Satisfies(i.Version, req) {
			return []string{"PATH=" + phpPath(i.Bin)}, "php " + i.Version.String() + " под require.php " + req
		}
	}
	return nil, "подходящего под " + req + " php нет: brew install php@<версия>"
}

type servePlan struct {
	p    *config.Project
	dir  string
	port string
	cmd  run.Command
	note string
}

func planServe(p *config.Project, port string) (servePlan, error) {
	dir := cfg.ProjectDir(p)
	facts := run.Detect(dir)
	facts.Custom = p.Serve
	c, err := run.Serve(facts, port)
	if err != nil {
		return servePlan{}, ui.Errorf("%s: %v", p.Name, err)
	}
	env, note := phpEnv(dir, facts.Stack)
	c.Env = append(c.Env, env...)
	switch {
	case note != "":
	case p.Serve != "":
		note = "своя команда"
	default:
		note = facts.Stack
	}
	return servePlan{p: p, dir: dir, port: port, cmd: c, note: note}, nil
}

var (
	servePortFlag int
	serveDetach   bool
)

var serveCmd = &cobra.Command{
	Use:   "serve [проект|часть имени|@группа...] [-d] [--port N]",
	Short: "Запустить dev-сервер проекта по его стеку; несколько или с -d - в фоне",
	Long: `Команда выбирается по стеку, поле serve проекта её перекрывает. Для PHP-проектов подставляется
версия php под require.php из composer.json. Несколько проектов (и группы [groups] из конфига) запускаются
в фоне, занятые порты заменяются следующими свободными.
Фоновые: tim serve list, tim serve logs <имя> [-f], tim serve stop [имя|all].`,
	ValidArgsFunction: projectNames,
	RunE: func(cmd *cobra.Command, args []string) error {
		port := ""
		if servePortFlag > 0 {
			port = strconv.Itoa(servePortFlag)
		}
		if len(args) == 2 {
			if _, err := strconv.Atoi(args[1]); err == nil {
				port, args = args[1], args[:1]
			}
		}
		list, err := resolveProjects(args)
		if err != nil {
			return err
		}
		if len(list) == 1 && !serveDetach {
			p := list[0]
			if port == "" {
				port = projectPort(p, cfg.ProjectDir(p))
			}
			if l, busy := netinfo.PortBusy(port); busy {
				return ui.Errorf("порт %s занят: %s (pid %s); освободить: tim port %s kill", port, l.Command, l.PID, port)
			}
			sp, err := planServe(p, port)
			if err != nil {
				return err
			}
			ui.Println(ui.Label(p.Name) + "  " + ui.Dim(sp.note+", порт "+port+", "+sp.dir))
			return runner.ExecIn(sp.dir, sp.cmd.Env, sp.cmd.Name, sp.cmd.Args...)
		}
		return serveBackground(list, port)
	},
}

func serveBackground(list []*config.Project, port string) error {
	var todo []*config.Project
	for _, p := range list {
		if st, ok := run.LoadState(p.Name); ok && st.Alive() {
			ui.Println(ui.Dim(p.Name + " уже работает на порту " + st.Port + ", pid " + strconv.Itoa(st.PID)))
			continue
		}
		todo = append(todo, p)
	}
	wanted := make([]string, len(todo))
	for i, p := range todo {
		wanted[i] = port
		if wanted[i] == "" || len(todo) > 1 {
			wanted[i] = projectPort(p, cfg.ProjectDir(p))
		}
	}
	ports := run.AllocPorts(wanted, func(p string) bool { _, busy := netinfo.PortBusy(p); return busy })
	failed := 0
	for i, p := range todo {
		sp, err := planServe(p, ports[i])
		if err != nil {
			failed++
			ui.Println(ui.Bad(err.Error()))
			continue
		}
		logPath := run.LogPath(p.Name)
		pid, err := runner.Start(sp.dir, sp.cmd.Env, logPath, sp.cmd.Name, sp.cmd.Args...)
		if err != nil {
			failed++
			ui.Println(ui.Bad(p.Name + ": не запустился: " + err.Error()))
			continue
		}
		line := ui.Label(ui.Pad(p.Name, 22)) + " порт " + ports[i] + ui.Dim("  "+sp.note)
		if ports[i] != wanted[i] {
			line += "  " + ui.Warn("вместо "+wanted[i]+" (занят); APP_URL мог остаться на "+wanted[i])
		}
		ui.Println(line)
		if runner.DryRun {
			continue
		}
		st := run.State{Name: p.Name, PID: pid, Port: ports[i], Dir: sp.dir, Command: sp.cmd.String(), Log: logPath, Started: time.Now()}
		if err := st.Save(); err != nil {
			return err
		}
	}
	if !runner.DryRun && len(todo) > failed {
		ui.Println(ui.Dim("tim serve list, tim serve logs <имя> -f, tim serve stop [имя|all]"))
	}
	if failed > 0 {
		return ui.Errorf("не запустились: %d из %d", failed, len(todo))
	}
	return nil
}

var serveListCmd = &cobra.Command{
	Use:   "list",
	Short: "Фоновые dev-серверы",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		states := run.ListStates()
		if len(states) == 0 {
			ui.Println(ui.Dim("фоновых серверов нет: tim serve <проект> -d"))
			return nil
		}
		for _, s := range states {
			state := ui.Label("работает")
			if !s.Alive() {
				state = ui.Bad("умер")
			}
			ui.Println(ui.Pad(s.Name, 22) + " " + ui.Pad(state, 10) + " порт " + ui.Pad(s.Port, 6) + ui.Dim(" pid "+strconv.Itoa(s.PID)+", "+human(time.Since(s.Started))+", "+s.Command))
		}
		return nil
	},
}

func stateNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	var names []string
	for _, s := range run.ListStates() {
		names = append(names, s.Name)
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

var serveStopCmd = &cobra.Command{
	Use:               "stop [имя|all]",
	Short:             "Остановить фоновый сервер вместе с дочерними процессами",
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: stateNames,
	RunE: func(cmd *cobra.Command, args []string) error {
		want := first(args)
		var targets []run.State
		for _, s := range run.ListStates() {
			if want == "all" || want == s.Name || want == "" {
				targets = append(targets, s)
			}
		}
		if want == "" && len(targets) > 1 {
			return ui.Errorf("фоновых несколько: tim serve stop <имя> или all")
		}
		if len(targets) == 0 {
			return ui.Errorf("фонового сервера %s нет: tim serve list", want)
		}
		for _, s := range targets {
			if s.Alive() {
				if err := runner.StopGroup(s.PID); err != nil {
					ui.Println(ui.Bad(s.Name + ": " + err.Error()))
					continue
				}
			}
			if !runner.DryRun {
				s.Remove()
			}
			ui.Println(ui.Label(s.Name + " остановлен"))
		}
		return nil
	},
}

var serveLogsFollow bool

var serveLogsCmd = &cobra.Command{
	Use:               "logs <имя> [-f]",
	Short:             "Вывод фонового сервера",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: stateNames,
	RunE: func(cmd *cobra.Command, args []string) error {
		path := run.LogPath(args[0])
		if _, err := os.Stat(path); err != nil {
			return ui.Errorf("журнала %s нет: tim serve list", args[0])
		}
		targs := []string{"-n", "50"}
		if serveLogsFollow {
			targs = append(targs, "-F")
		}
		return runner.Exec("tail", append(targs, path)...)
	},
}

var importantEnv = []string{"APP_NAME", "APP_ENV", "APP_DEBUG", "APP_URL", "DB_CONNECTION", "DB_HOST", "DB_PORT", "DB_DATABASE",
	"DB_USERNAME", "DATABASE_URL", "REDIS_HOST", "REDIS_PORT", "CACHE_STORE", "CACHE_DRIVER", "QUEUE_CONNECTION",
	"SESSION_DRIVER", "MAIL_MAILER", "MAIL_HOST", "MEILISEARCH_HOST", "SCOUT_DRIVER"}

var envAll bool

var envCmd = &cobra.Command{
	Use:               "env [проект] [--all]",
	Short:             "Выжимка .env без секретов и сверка с реестром",
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: projectNames,
	RunE: func(cmd *cobra.Command, args []string) error {
		p, err := projectFor(first(args))
		if err != nil {
			return err
		}
		file := filepath.Join(cfg.ProjectDir(p), ".env")
		vals, err := envfile.Parse(file)
		if err != nil {
			ui.Println(ui.Dim(p.Name + ": .env нет"))
			return nil
		}
		ui.Println(ui.Label(p.Name) + "  " + ui.Dim(file))
		keys := importantEnv
		if envAll {
			keys = make([]string, 0, len(vals))
			for k := range vals {
				keys = append(keys, k)
			}
			sort.Strings(keys)
		}
		for _, k := range keys {
			if v, ok := vals[k]; ok {
				ui.Println(ui.Label(ui.Pad(k, 20)) + " " + envfile.Mask(k, v))
			}
		}
		if db := vals["DB_DATABASE"]; p.DB != "" && db != "" && !strings.HasSuffix(p.DB, db) {
			ui.Println(ui.Warn("база в реестре " + p.DB + ", в .env " + db))
		}
		if port := envfile.Port(vals["APP_URL"]); p.Port > 0 && port != "" && port != strconv.Itoa(p.Port) {
			ui.Println(ui.Warn("порт в реестре " + strconv.Itoa(p.Port) + ", в APP_URL " + port))
		}
		if !envAll {
			ui.Println(ui.Dim("все ключи: tim env --all (секреты скрыты)"))
		}
		return nil
	},
}

var (
	logsFollow bool
	logsLines  int
)

// newestLog: самый свежий *.log в привычных папках журналов проекта.
func newestLog(dir string) string {
	best, bestT := "", time.Time{}
	for _, sub := range []string{"storage/logs", "logs", "log", "var/log", "."} {
		files, _ := filepath.Glob(filepath.Join(dir, sub, "*.log"))
		for _, f := range files {
			if st, err := os.Stat(f); err == nil && st.ModTime().After(bestT) {
				best, bestT = f, st.ModTime()
			}
		}
	}
	return best
}

var logsCmd = &cobra.Command{
	Use:               "logs [проект] [-f] [-n 50]",
	Short:             "Хвост самого свежего журнала проекта",
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: projectNames,
	RunE: func(cmd *cobra.Command, args []string) error {
		p, err := projectFor(first(args))
		if err != nil {
			return err
		}
		file := newestLog(cfg.ProjectDir(p))
		if file == "" {
			return ui.Errorf("%s: журналов *.log не нашёл (storage/logs, logs, log, var/log)", p.Name)
		}
		ui.Println(ui.Dim(file))
		targs := []string{"-n", strconv.Itoa(logsLines)}
		if logsFollow {
			targs = append(targs, "-F")
		}
		return runner.Exec("tail", append(targs, file)...)
	},
}

var openCmd = &cobra.Command{
	Use:               "open [проект]",
	Short:             "Открыть проект в редакторе (ключ editor в конфиге, $VISUAL или Finder)",
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: projectNames,
	RunE: func(cmd *cobra.Command, args []string) error {
		p, err := projectFor(first(args))
		if err != nil {
			return err
		}
		dir := cfg.ProjectDir(p)
		editor := cfg.Editor
		if editor == "" {
			editor = os.Getenv("VISUAL")
		}
		if editor == "" {
			return runner.Run("open", dir)
		}
		parts := strings.Fields(editor)
		return runner.Run(parts[0], append(parts[1:], dir)...)
	},
}

func init() {
	envCmd.Flags().BoolVar(&envAll, "all", false, "все ключи, секреты скрыты")
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "следить за новыми строками")
	logsCmd.Flags().IntVarP(&logsLines, "lines", "n", 50, "сколько строк")
	serveCmd.Flags().BoolVarP(&serveDetach, "detach", "d", false, "в фоне")
	serveCmd.Flags().IntVar(&servePortFlag, "port", 0, "порт (для одного проекта)")
	serveLogsCmd.Flags().BoolVarP(&serveLogsFollow, "follow", "f", false, "следить")
	serveCmd.AddCommand(serveListCmd, serveStopCmd, serveLogsCmd)
	root.AddCommand(serveCmd, envCmd, logsCmd, openCmd)
}
