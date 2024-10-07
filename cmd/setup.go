package cmd

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/envfile"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/newproj"
	"github.com/TimurTurdyev/tim/internal/registry"
	"github.com/TimurTurdyev/tim/internal/run"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/setup"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var (
	setupYes  bool
	setupIn   string
	setupName string
)

// setupState: что уже есть в папке проекта, для построения плана.
func setupState(dir string) setup.State {
	f := run.Detect(dir)
	s := setup.State{Stack: f.Stack, PkgManager: f.PkgManager, Has: map[string]bool{}, Env: map[string]string{}}
	for _, name := range []string{
		".env", ".env.example", ".env.sample", ".env.dist", "composer.json", "vendor",
		"package.json", "node_modules", "go.mod", "uv.lock", "pyproject.toml",
		"requirements.txt", ".venv", "venv", "storage", "public/storage",
	} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			s.Has[name] = true
		}
	}
	if env, err := envfile.Parse(filepath.Join(dir, ".env")); err == nil {
		s.Env = env
	}
	// Файл базы sqlite проверяем по тому пути, который назван в .env.
	if db := s.Env["DB_DATABASE"]; strings.HasSuffix(db, ".sqlite") && !strings.HasPrefix(db, "/") {
		if _, err := os.Stat(filepath.Join(dir, db)); err == nil {
			s.Has[db] = true
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "database", "database.sqlite")); err == nil {
		s.Has["database/database.sqlite"] = true
	}
	return s
}

// gitURL: аргумент похож на адрес репозитория, а не на имя проекта.
func gitURL(s string) bool {
	return strings.HasSuffix(s, ".git") || strings.HasPrefix(s, "git@") ||
		strings.HasPrefix(s, "https://") && strings.Count(s, "/") >= 4
}

// repoName: имя проекта из адреса репозитория.
func repoName(url string) string {
	name := strings.TrimSuffix(url, ".git")
	if i := strings.LastIndexAny(name, "/:"); i >= 0 {
		name = name[i+1:]
	}
	return name
}

// setupClone: клонирует репозиторий по раскладке реестра и записывает проект.
func setupClone(url string) (*config.Project, string, error) {
	name := setupName
	if name == "" {
		name = repoName(url)
	}
	if name == "" {
		return nil, "", ui.Errorf("не понял имя проекта из %s: задай --name", url)
	}
	if _, ok := cfg.Project(name); ok {
		return nil, "", ui.Errorf("проект %s уже есть в реестре: tim setup %s", name, name)
	}
	dir, parent := newproj.Layout(cfg.Dev, setupIn, name, cfg.Wrapper)
	if _, err := os.Stat(dir); err == nil {
		return nil, "", ui.Errorf("папка уже есть: %s", dir)
	}
	ui.Println(ui.Label(name) + ui.Dim("  "+url+" -> "+dir))
	if runner.DryRun {
		ui.Println(ui.Dim("$ mkdir -p " + parent))
	} else if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, "", err
	}
	if err := runner.ExecIn(parent, nil, "git", "clone", url, dir); err != nil {
		return nil, "", ui.Errorf("git clone: %v", err)
	}
	p := config.Project{Name: name, Path: cfg.Relative(dir), Remote: url}
	return &p, dir, nil
}

var setupCmd = &cobra.Command{
	Use:   "setup [проект|адрес репозитория]",
	Short: "От клона до работающего локально: зависимости, .env, ключ, база",
	Long: `Разбирает, чего проекту не хватает, и доделывает: копирует .env из примера, ставит
зависимости php, js, go и python, у Laravel генерирует ключ, создаёт файл базы sqlite,
накатывает миграции и ставит ссылку на хранилище. Уже сделанное не повторяется.
Адрес репозитория вместо имени сначала клонирует его по раскладке реестра и добавляет проект.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var (
			p     *config.Project
			dir   string
			fresh bool
			err   error
		)
		if len(args) == 1 && gitURL(args[0]) {
			if p, dir, err = setupClone(args[0]); err != nil {
				return err
			}
			fresh = true
		} else {
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			if p, err = registry.Pick(cfg, name); err != nil {
				return err
			}
			dir = cfg.ProjectDir(p)
			ui.Println(ui.Label(p.Name) + ui.Dim("  "+dir))
		}
		steps := setup.Plan(setupState(dir))
		if len(steps) == 0 {
			ui.Println(ui.Dim("всё на месте, делать нечего"))
			return setupFinish(p, dir, fresh)
		}
		width := 0
		for _, s := range steps {
			width = max(width, len([]rune(s.Why)))
		}
		for _, s := range steps {
			ui.Println("  " + ui.Label(ui.Pad(s.Why, width)) + "  " + ui.Dim(s.String()))
		}
		if !setupYes && !runner.Confirm("выполнить?") {
			return ui.Errorf("отменено")
		}
		for _, s := range steps {
			if s.Ask && !setupYes && !runner.Confirm(s.Why+": "+s.String()+"?") {
				continue
			}
			if s.Touch != "" {
				if err := touchFile(filepath.Join(dir, s.Touch)); err != nil {
					return err
				}
				continue
			}
			if s.Cmd == nil {
				if err := copyFile(filepath.Join(dir, s.Copy[0]), filepath.Join(dir, s.Copy[1])); err != nil {
					return err
				}
				continue
			}
			if err := runner.ExecIn(dir, nil, s.Cmd[0], s.Cmd[1:]...); err != nil {
				return ui.Errorf("%s: %s: %v", s.Why, s.String(), err)
			}
		}
		return setupFinish(p, dir, fresh)
	},
}

func touchFile(path string) error {
	if runner.DryRun {
		ui.Println(ui.Dim("$ mkdir -p " + filepath.Dir(path) + " && touch " + path))
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	return f.Close()
}

func copyFile(src, dst string) error {
	if runner.DryRun {
		ui.Println(ui.Dim("$ cp " + src + " " + dst))
		return nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o600)
}

// setupFinish: новый проект попадает в реестр уже с портом и базой, подсмотренными у него самого.
func setupFinish(p *config.Project, dir string, fresh bool) error {
	if fresh {
		// Порт выдаём только тем, кого потом можно поднять: репозиторию без стека он ни к чему.
		if _, err := run.Serve(run.Detect(dir), "8000"); err == nil && p.Port == 0 {
			var used []int
			for _, q := range cfg.Projects {
				if q.Port > 0 {
					used = append(used, q.Port)
				}
			}
			p.Port = newproj.NextPort(used, 8000, func(s string) bool { _, b := netinfo.PortBusy(s); return b })
		}
		if err := cfg.AddProject(*p); err != nil {
			return err
		}
		if err := saveConfig(cfg); err != nil {
			return err
		}
	}
	line := ui.Label("готово ") + p.Name
	if p.Port > 0 {
		line += ui.Dim("  порт " + strconv.Itoa(p.Port))
	}
	ui.Println(line)
	ui.Println(ui.Dim("дальше: tim serve " + p.Name + ", tim go " + p.Name))
	return nil
}

func init() {
	setupCmd.Flags().BoolVarP(&setupYes, "yes", "y", false, "не спрашивать подтверждения")
	setupCmd.Flags().StringVar(&setupIn, "in", "projects", "папка внутри корня проектов при клонировании")
	setupCmd.Flags().StringVar(&setupName, "name", "", "имя проекта при клонировании")
	root.AddCommand(setupCmd)
}
