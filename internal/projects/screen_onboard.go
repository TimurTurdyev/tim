package projects

import (
	"os"
	"path/filepath"
	"time"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/brand"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/rcfile"
	"github.com/TimurTurdyev/tim/internal/run"
	"github.com/TimurTurdyev/tim/internal/scan"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Первый запуск без конфига: мастер вместо главного экрана. Логика та же, что у tim config init.

// OnboardScreen: мастер первого запуска; next - экран после него, nil - выход.
func OnboardScreen(env *app.Env, next func() tui.Screen) tui.Screen {
	o := tui.OnboardOpts{
		Roots: onboardRoots,
		Scan:  onboardScan,
		Save:  func(root string, chosen []tui.FoundProject) (string, error) { return onboardSave(env, root, chosen) },
		Shell: onboardShell,
		ShellLine: func() string {
			path, shell := rcfile.Path("")
			return `строку eval "$(tim init ` + shell + `)" в ` + paths.Show(path)
		}(),
		Home: next,
	}
	if on, depth := ui.Colors(); on {
		o.Art = brand.MascotLarge(brand.Look{Mood: brand.Calm, Light: ui.Light()}, depth)
	}
	return tui.NewOnboard(o)
}

// onboardRoots: привычные папки с кодом, в которых что-то есть.
func onboardRoots() []tui.RootChoice {
	home := paths.Home()
	var out []tui.RootChoice
	candidates := []string{"Dev", "Projects", "projects", "code", "src", "www", "work", "repos", "git"}
	for _, d := range candidates {
		p := filepath.Join(home, d)
		if st, err := os.Stat(p); err != nil || !st.IsDir() {
			continue
		}
		out = append(out, tui.RootChoice{Path: paths.Show(p), Note: rootNote(p)})
	}
	for _, p := range []string{"/var/www", "/srv"} {
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			out = append(out, tui.RootChoice{Path: p, Note: rootNote(p)})
		}
	}
	return out
}

func rootNote(root string) string {
	clones, err := scan.Clones(root, scanDepth)
	if err != nil || len(clones) == 0 {
		return "репозиториев не видно"
	}
	var last time.Time
	for _, c := range clones {
		if t := gitTouched(c); t.After(last) {
			last = t
		}
	}
	note := ui.Count(len(clones), "репозиторий", "репозитория", "репозиториев")
	if !last.IsZero() {
		note += ", последний менялся " + ui.Short(time.Since(last))
	}
	return note
}

// gitTouched: когда в клоне что-то делали - по файлам git, без запуска самого git.
func gitTouched(dir string) time.Time {
	var last time.Time
	for _, f := range []string{".git/index", ".git/HEAD", ".git/FETCH_HEAD", ""} {
		if st, err := os.Stat(filepath.Join(dir, f)); err == nil && st.ModTime().After(last) {
			last = st.ModTime()
		}
	}
	return last
}

func onboardConfig(root string) *config.Config {
	c := config.Default()
	c.Dev = config.ExpandHome(root)
	if abs, err := filepath.Abs(c.Dev); err == nil {
		c.Dev = abs
	}
	c.Wrapper = guessWrapper(c.Dev)
	return c
}

func onboardScan(root string) []tui.FoundProject {
	c := onboardConfig(root)
	found, err := newClones(c)
	if err != nil {
		return nil
	}
	var out []tui.FoundProject
	for _, p := range found {
		dir := c.ProjectDir(&p)
		touched := gitTouched(dir)
		fp := tui.FoundProject{Name: p.Name, Stack: ui.Dash(run.Stack(dir)), Dir: paths.Show(dir), Checked: true}
		if !touched.IsZero() {
			fp.Age = ui.Short(time.Since(touched))
			// Год без изменений - скорее всего архив; галочку снимаем, вернуть можно пробелом.
			fp.Checked = time.Since(touched) < 365*24*time.Hour
		}
		out = append(out, fp)
	}
	return out
}

func onboardSave(env *app.Env, root string, chosen []tui.FoundProject) (string, error) {
	c := onboardConfig(root)
	for _, p := range chosen {
		dir := config.ExpandHome(p.Dir)
		if err := c.AddProject(config.Project{Name: p.Name, Path: c.Relative(dir)}); err != nil {
			return "", err
		}
	}
	env.Cfg = c
	if err := env.Save(); err != nil {
		return "", err
	}
	return "в реестре " + ui.Count(len(c.Projects), "проект", "проекта", "проектов") +
		", конфиг " + paths.Show(config.Path()), nil
}

// onboardShell: та же строка, что дописывает tim init.
func onboardShell() (string, error) {
	path, shell := rcfile.Path("")
	if rcfile.Sourced(path, 2) {
		return "оболочка уже подключена", nil
	}
	if err := rcfile.Append(path, `eval "$(tim init `+shell+`)"`); err != nil {
		return "", err
	}
	return "подключено в " + paths.Show(path) + ", открой новый терминал", nil
}
