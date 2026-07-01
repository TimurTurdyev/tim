package serve

import (
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/newproj"
	"github.com/TimurTurdyev/tim/internal/run"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// serve: один проект - на переднем плане, несколько или с -d - в фоне.
func serve(env *app.Env, args []string, o options) error {
	port := ""
	if o.port > 0 {
		port = strconv.Itoa(o.port)
	}
	if len(args) == 2 {
		if _, err := strconv.Atoi(args[1]); err == nil {
			port, args = args[1], args[:1]
		}
	}
	list, err := env.Projects(args)
	if err != nil {
		return err
	}
	if len(list) == 1 && !o.detach {
		return serveForeground(env, list[0], port, o)
	}
	return background(env, list, port, o)
}

func serveForeground(env *app.Env, p *config.Project, port string, o options) error {
	dir := env.Cfg.ProjectDir(p)
	if port == "" {
		port = pinPort(env, p, dir)
	}
	if l, busy := netinfo.PortBusy(port); busy {
		return ui.Errorf("порт %s занят: %s (pid %s); освободить: tim port %s kill", port, l.Command, l.PID, port)
	}
	sp, err := run.NewPlan(p, dir, port)
	if err != nil {
		return err
	}
	ui.Println(ui.Label(p.Name) + "  " + ui.Dim(sp.Note+", порт "+port+", "+sp.Dir))
	if o.https {
		f, err := planHTTPS(p, map[string]bool{port: true})
		if err != nil {
			return err
		}
		if err := startHTTPSInline(f, port); err != nil {
			return err
		}
		line := "  " + f.url()
		if hint := httpsHint(run.Stack(sp.Dir)); hint != "" {
			line += ui.Dim("  " + hint)
		}
		ui.Println(line)
		if o.open {
			go openWhenReady(f.port, f.url())
		}
	} else if o.open {
		go openWhenReady(port, "http://localhost:"+port)
	}
	return runner.ExecIn(sp.Dir, sp.Cmd.Env, sp.Cmd.Name, sp.Cmd.Args...)
}

// background запускает проекты в фоне; port - для одного проекта, занятые порты заменяются свободными.
func background(env *app.Env, list []*config.Project, port string, o options) error {
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
			wanted[i] = pinPort(env, p, env.Cfg.ProjectDir(p))
		}
	}
	ports := run.AllocPorts(wanted, netinfo.BusyPorts().Busy)
	taken := map[string]bool{}
	for _, p := range ports {
		taken[p] = true
	}
	failed := 0
	for i, p := range todo {
		if err := startOne(env, p, ports[i], wanted[i], taken, o); err != nil {
			failed++
			ui.Println(ui.Bad(err.Error()))
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

// startOne: один проект в фоне, рядом его https-прокси; состояние - чтобы потом найти и остановить.
func startOne(env *app.Env, p *config.Project, port, wanted string, taken map[string]bool, o options) error {
	sp, err := run.NewPlan(p, env.Cfg.ProjectDir(p), port)
	if err != nil {
		return err
	}
	logPath := run.LogPath(p.Name)
	pid, err := runner.Start(sp.Dir, sp.Cmd.Env, logPath, sp.Cmd.Name, sp.Cmd.Args...)
	if err != nil {
		return ui.Errorf("%s: не запустился: %v", p.Name, err)
	}
	line := ui.Label(ui.Pad(p.Name, 22)) + " порт " + port + ui.Dim("  "+sp.Note)
	if port != wanted {
		line += "  " + ui.Warn("вместо "+wanted+" (занят); APP_URL мог остаться на "+wanted)
	}
	ui.Println(line)
	if o.https {
		backgroundHTTPS(env, p, port, taken, o.open)
	} else if o.open {
		openWhenReady(port, "http://localhost:"+port)
	}
	if runner.DryRun {
		return nil
	}
	st := run.State{Name: p.Name, PID: pid, Port: port, Dir: sp.Dir, Command: sp.Cmd.String(), Log: logPath, Started: time.Now()}
	return st.Save()
}

func backgroundHTTPS(env *app.Env, p *config.Project, port string, taken map[string]bool, open bool) {
	f, err := planHTTPS(p, taken)
	if err != nil {
		ui.Println(ui.Warn("  " + err.Error()))
		return
	}
	if err := startHTTPSBackground(env.Cfg.ProjectDir(p), p.Name, f, port); err != nil {
		ui.Println(ui.Bad("  https-прокси: " + err.Error()))
		return
	}
	ui.Println("  " + f.url())
	if open {
		openWhenReady(f.port, f.url())
	}
}

// pinPort: порт для запуска. У проекта без порта и без порта в APP_URL tim закрепляет свой,
// которого нет ни у кого в реестре, и записывает его: иначе у всех таких проектов будет 8000
// и при запуске вместе второй молча уедет на другой порт.
func pinPort(env *app.Env, p *config.Project, dir string) string {
	port := run.ProjectPort(p, dir)
	if p.Port > 0 || port != netinfo.Port(netinfo.DefaultPort) {
		return port
	}
	n := newproj.NextPort(env.Cfg.Ports(), netinfo.DefaultPort, netinfo.BusyPorts().Busy)
	p.Port = n
	if err := env.Save(); err != nil {
		ui.Println(ui.Warn("порт не закреплён: " + err.Error()))
		return strconv.Itoa(n)
	}
	if !runner.DryRun {
		ui.Println(ui.Dim("закрепил за " + p.Name + " порт " + strconv.Itoa(n) + "; поменять - поле port в реестре"))
	}
	return strconv.Itoa(n)
}

// openWhenReady открывает адрес, как только порт ответит. Поле url проекта не берём:
// там часто боевой адрес, а открыть надо локальный.
func openWhenReady(port, url string) {
	if runner.DryRun {
		_ = runner.OpenBrowser(url)
		return
	}
	if !netinfo.WaitPort(port, 30*time.Second) {
		ui.Println(ui.Warn("порт " + port + " за 30 с не ответил, браузер не открываю"))
		return
	}
	if err := runner.OpenBrowser(url); err != nil {
		ui.Println(ui.Warn("не открыл " + url + ": " + err.Error()))
	}
}

func printBackground() {
	states := run.ListStates()
	if len(states) == 0 {
		ui.Println(ui.Dim("фоновых серверов нет: tim serve <проект> -d"))
		return
	}
	for _, s := range states {
		state := ui.Label("работает")
		if !s.Alive() {
			state = ui.Bad("умер")
		}
		ui.Println(ui.Pad(s.Name, 22) + " " + ui.Pad(state, 10) + " порт " + ui.Pad(s.Port, 6) +
			ui.Dim(" pid "+strconv.Itoa(s.PID)+", "+ui.Duration(time.Since(s.Started))+", "+s.Command))
	}
}

// stop гасит фоновый сервер вместе с его https-прокси (<проект>.https); all - все.
func stop(want string) error {
	var targets []run.State
	for _, s := range run.ListStates() {
		if want == "all" || want == s.Name || s.Name == want+".https" || want == "" {
			targets = append(targets, s)
		}
	}
	withProxy := len(targets) == 2 && targets[1].Name == targets[0].Name+".https"
	if want == "" && len(targets) > 1 && !withProxy {
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
}

func backgroundLog(name string, follow bool) error {
	path := run.LogPath(name)
	if _, err := os.Stat(path); err != nil {
		return ui.Errorf("журнала %s нет: tim serve list", name)
	}
	return tail(50, follow, path)
}

// projectLog: хвост самого свежего журнала проекта.
func projectLog(env *app.Env, name string, lines int, follow bool) error {
	p, err := env.Project(name)
	if err != nil {
		return err
	}
	file := newestLog(env.Cfg.ProjectDir(p))
	if file == "" {
		return ui.Errorf("%s: журналов *.log не нашёл (storage/logs, logs, log, var/log)", p.Name)
	}
	ui.Println(ui.Dim(file))
	return tail(lines, follow, file)
}

func tail(lines int, follow bool, files ...string) error {
	args := []string{"-n", strconv.Itoa(lines)}
	if follow {
		args = append(args, "-F")
	}
	return runner.Exec("tail", append(args, files...)...)
}

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

// openEditor: проект в редакторе.
func openEditor(env *app.Env, name string) error {
	p, err := env.Project(name)
	if err != nil {
		return err
	}
	return env.Edit(env.Cfg.ProjectDir(p))
}
