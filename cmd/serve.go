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

var serveCmd = &cobra.Command{
	Use:               "serve [проект] [порт]",
	Short:             "Запустить dev-сервер проекта по его стеку",
	Args:              cobra.MaximumNArgs(2),
	ValidArgsFunction: projectNames,
	RunE: func(cmd *cobra.Command, args []string) error {
		p, err := projectFor(first(args))
		if err != nil {
			return err
		}
		dir := cfg.ProjectDir(p)
		port := second(args)
		if port == "" {
			port = projectPort(p, dir)
		}
		if l, busy := netinfo.PortBusy(port); busy {
			return ui.Errorf("порт %s занят: %s (pid %s); освободить: tim port %s kill", port, l.Command, l.PID, port)
		}
		facts := run.Detect(dir)
		c, err := run.Serve(facts, port)
		if err != nil {
			return ui.Errorf("%s: %v", p.Name, err)
		}
		ui.Println(ui.Label(p.Name) + "  " + ui.Dim(facts.Stack+", порт "+port+", "+dir))
		return runner.ExecIn(dir, c.Env, c.Name, c.Args...)
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
	root.AddCommand(serveCmd, envCmd, logsCmd, openCmd)
}
