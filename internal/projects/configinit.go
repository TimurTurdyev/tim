package projects

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/scan"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// scanDepth: на сколько уровней под корнем искать клоны.
const scanDepth = 4

// printConfig: где конфиг и что в нём - сводкой; весь файл - с raw. Сотни строк [[project]] на
// вопрос "что в конфиге" не отвечают.
func printConfig(cfg *config.Config, raw bool) error {
	if raw {
		ui.Println(ui.Dim(config.Path()))
		return toml.NewEncoder(ui.Writer()).Encode(cfg)
	}
	label := func(s string) string { return ui.Label(ui.Pad(s, 10)) }
	ui.Println(label("конфиг") + config.Path())
	root := cfg.Dev
	if cfg.Wrapper != "" {
		root += ui.Dim("  клон в подпапке " + cfg.Wrapper)
	}
	ui.Println(label("корень") + root)
	missing := 0
	for i := range cfg.Projects {
		if _, err := os.Stat(cfg.ProjectDir(&cfg.Projects[i])); err != nil {
			missing++
		}
	}
	line := ui.Count(len(cfg.Projects), "проект", "проекта", "проектов")
	if missing > 0 {
		line += ui.Warn(", без папки "+strconv.Itoa(missing)) + ui.Dim(" - tim doctor")
	}
	ui.Println(label("реестр") + line)
	if len(cfg.Hosts) > 0 {
		ui.Println(label("обход") + ui.Count(len(cfg.Hosts), "цель", "цели", "целей") + ui.Dim(" - tim route"))
	}
	if len(cfg.Tasks) > 0 {
		ui.Println(label("задачи") + strconv.Itoa(len(cfg.Tasks)) + ui.Dim(" - tim each"))
	}
	if len(cfg.Groups) > 0 {
		ui.Println(label("группы") + strconv.Itoa(len(cfg.Groups)))
	}
	sh := []string{"сводка при входе " + onOff(cfg.Shell.Motd), "проект при cd " + onOff(cfg.Shell.CdInfoOn())}
	if len(cfg.Shell.Aliases) > 0 {
		sh = append(sh, "алиасов "+strconv.Itoa(len(cfg.Shell.Aliases)))
	}
	ui.Println(label("оболочка") + strings.Join(sh, ", "))
	ui.Println(ui.Dim("весь файл: tim config --raw; править можно руками, tim перечитывает сам"))
	return nil
}

func onOff(on bool) string {
	if on {
		return "да"
	}
	return "нет"
}

// initConfig: мастер первого конфига. Без конфига в терминале - экран первого запуска.
func initConfig(env *app.Env, o initOptions) error {
	_, statErr := os.Stat(config.Path())
	exists := statErr == nil
	if !exists && app.Interactive() && !o.yes && o.root == "" && !o.all {
		return env.Open(OnboardScreen(env, env.Home))
	}
	c := config.Default()
	switch {
	case exists && o.merge:
		c = env.Cfg
	case exists && !o.yes:
		if !runner.Confirm("конфиг " + config.Path() + " уже есть, перезаписать? (дополнить: --merge)") {
			return nil
		}
	}
	interactive := app.Interactive() && !o.yes
	root, err := askRoot(c.Dev, o.root, interactive)
	if err != nil {
		return err
	}
	c.Dev = root
	if c.Wrapper, err = askWrapper(c, o, interactive); err != nil {
		return err
	}
	found, err := newClones(c)
	if err != nil {
		return err
	}
	ui.Println(ui.Label("корень ") + c.Dev + ui.Dim("  найдено новых клонов: "+strconv.Itoa(len(found))))
	chosen := found
	if !o.all {
		if chosen, err = chooseProjects(found, o.yes); err != nil {
			return err
		}
	}
	n := addAll(c, chosen)
	env.Cfg = c
	if err := env.Save(); err != nil {
		return err
	}
	ui.Println(ui.Label("записан ") + config.Path() + ui.Dim("  проектов в реестре: "+strconv.Itoa(len(c.Projects))+", добавлено "+strconv.Itoa(n)))
	ui.Println(ui.Dim("дальше: tim init подключит tim к shell, потом tim go"))
	return nil
}

func askRoot(current, flag string, interactive bool) (string, error) {
	root := current
	switch {
	case flag != "":
		root = flag
	case interactive:
		r, err := app.Prompt("Корень, под которым лежат проекты", current, "--root")
		if err != nil {
			return "", err
		}
		root = r
	}
	abs, err := filepath.Abs(config.ExpandHome(root))
	if err != nil {
		return "", err
	}
	if st, err := os.Stat(abs); err != nil || !st.IsDir() {
		return "", ui.Errorf("папки нет: %s", abs)
	}
	return abs, nil
}

func askWrapper(c *config.Config, o initOptions, interactive bool) (string, error) {
	switch {
	case o.wrapperSet:
		return o.wrapper, nil
	case !interactive && c.Wrapper == "":
		return guessWrapper(c.Dev), nil
	case interactive:
		guess := c.Wrapper
		if guess == "" {
			guess = guessWrapper(c.Dev)
		}
		return app.Prompt("Подпапка с клоном внутри папки проекта (пусто, если клон лежит прямо в ней)", guess, "--wrapper")
	}
	return c.Wrapper, nil
}

// guessWrapper: если у нескольких клонов одинаковое имя папки (app, src), это скорее всего обёртка.
func guessWrapper(root string) string {
	clones, err := scan.Clones(root, scanDepth)
	if err != nil {
		return ""
	}
	count := map[string]int{}
	for _, c := range clones {
		count[filepath.Base(c)]++
	}
	best, bestN := "", 1
	for name, n := range count {
		if n > bestN || n == bestN && name < best {
			best, bestN = name, n
		}
	}
	return best
}

// newClones: клоны под корнем, которых нет в реестре, с предложенными именами.
func newClones(c *config.Config) ([]config.Project, error) {
	clones, err := scan.Clones(c.Dev, scanDepth)
	if err != nil {
		return nil, ui.Errorf("поиск в %s: %v", c.Dev, err)
	}
	known := map[string]bool{}
	for i := range c.Projects {
		known[filepath.Clean(c.ProjectDir(&c.Projects[i]))] = true
	}
	var fresh []string
	for _, cl := range clones {
		if !known[filepath.Clean(cl)] {
			fresh = append(fresh, cl)
		}
	}
	names := scan.Names(fresh, c.Wrapper, takenNames(c))
	out := make([]config.Project, len(fresh))
	for i, cl := range fresh {
		out[i] = config.Project{Name: names[i], Path: c.Relative(cl)}
	}
	return out, nil
}

// chooseProjects: all - все найденные; иначе выбор в терминале (всё отмечено), без терминала - ничего.
func chooseProjects(found []config.Project, all bool) ([]config.Project, error) {
	if all {
		return found, nil
	}
	if !app.Interactive() {
		return nil, nil
	}
	opts := make([]app.Option, len(found))
	sel := make([]bool, len(found))
	for i, p := range found {
		opts[i] = app.Option{Title: p.Name, Hint: p.Path}
		sel[i] = true
	}
	idx, err := app.ChooseManyFrom("Какие проекты добавить в реестр?", opts, sel)
	if err != nil {
		return nil, app.PickErr(err, "--all", nil)
	}
	out := make([]config.Project, 0, len(idx))
	for _, i := range idx {
		out = append(out, found[i])
	}
	return out, nil
}

func addAll(c *config.Config, list []config.Project) int {
	n := 0
	for _, p := range list {
		if c.AddProject(p) == nil {
			n++
		}
	}
	return n
}
