package cmd

import (
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/TimurTurdyev/tim/internal/brand"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/run"
	"github.com/TimurTurdyev/tim/internal/scan"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Первый запуск без конфига: мастер вместо главного экрана. Логика та же, что у tim config init.

func onboardScreen() tui.Screen {
	o := tui.OnboardOpts{
		Roots: onboardRoots,
		Scan:  onboardScan,
		Save:  onboardSave,
		Shell: onboardShell,
		Home:  func() tui.Screen { return tui.NewHome(homeOpts()) },
	}
	if on, depth := ui.Colors(); on {
		o.Art = brand.Raccoon(brand.Calm, depth)
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
		out = append(out, tui.RootChoice{Path: showPath(p), Note: rootNote(p)})
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
	note := strconv.Itoa(len(clones)) + " " + ui.Plural(len(clones), "репозиторий", "репозитория", "репозиториев")
	if !last.IsZero() {
		note += ", последний менялся " + agoShort(last)
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
		fp := tui.FoundProject{Name: p.Name, Stack: dashIfEmpty(run.Stack(dir)), Dir: showPath(dir), Checked: true}
		if !touched.IsZero() {
			fp.Age = agoShort(touched)
			// Год без изменений - скорее всего архив; галочку снимаем, вернуть можно пробелом.
			fp.Checked = time.Since(touched) < 365*24*time.Hour
		}
		out = append(out, fp)
	}
	return out
}

func onboardSave(root string, chosen []tui.FoundProject) (string, error) {
	c := onboardConfig(root)
	for _, p := range chosen {
		dir := config.ExpandHome(p.Dir)
		if err := c.AddProject(config.Project{Name: p.Name, Path: c.Relative(dir)}); err != nil {
			return "", err
		}
	}
	if err := saveConfig(c); err != nil {
		return "", err
	}
	cfg = c
	return "в реестре " + strconv.Itoa(len(c.Projects)) + " " + ui.Plural(len(c.Projects), "проект", "проекта", "проектов") +
		", конфиг " + showPath(config.Path()), nil
}

// onboardShell: та же строка, что дописывает tim init.
func onboardShell() (string, error) {
	path, shell := rcFile("")
	if sourced(path, 2) {
		return "оболочка уже подключена", nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString("\n# tim\neval \"$(tim init " + shell + ")\"\n"); err != nil {
		_ = f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return "подключено в " + showPath(path) + ", открой новый терминал", nil
}
