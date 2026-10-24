// Package home: экранный режим - главный экран, строка ":" с любой командой tim и экран результата.
package home

import (
	"errors"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/projects"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/shellq"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
	"github.com/TimurTurdyev/tim/internal/visits"
)

// notRemembered: команды, которые не предлагать в недавнем: справка, служебные, шим, сводка терминала.
var notRemembered = map[string]bool{"help": true, "completion": true, "__complete": true, "__completeNoDesc": true,
	"version": true, "init": true, "status": true, "go": true, "ui": true}

// argsHidden: команды, чьи аргументы не пишутся в недавнее - в них бывают токены, заголовки и чужие команды.
var argsHidden = map[string]bool{"http": true, "ssh": true, "each": true, "hook": true}

// Home: главный экран и строка ":". Команды выполняются тем же деревом cobra, что и из терминала.
type Home struct {
	env       *app.Env
	root      *cobra.Command
	examples  map[string]string
	overview  func()
	translate func(string) string
	prepare   func(args []string)
	utils     []string
	entries   func() []tui.CommandEntry
}

// Options: что экранному режиму нужно от корня.
type Options struct {
	Examples  map[string]string   // примеры из справки - тоже подсказки для ":"
	Overview  func()              // заставка, когда экрана открыть негде
	Translate func(string) string // ошибки cobra по-русски
	Prepare   func(args []string) // перед командой из экрана: например, русская справка
	Utils     []string            // мелкие утилиты для раздела "Утилиты"
}

// New связывает экраны с командами: окружение получает запуск команд из экрана и главный экран.
func New(env *app.Env, root *cobra.Command, o Options) *Home {
	h := &Home{env: env, root: root, examples: o.Examples, overview: o.Overview, translate: o.Translate,
		prepare: o.Prepare, utils: o.Utils}
	// Дерево команд не меняется после запуска: подсказки собираются один раз, а не на каждую букву.
	h.entries = sync.OnceValue(h.collectCommands)
	env.Result, env.After, env.Open, env.Home = h.result, h.after, h.open, h.screen
	env.Inside, env.ProjectsScreen = h.inside, h.projectsScreen
	return h
}

// Command: tim ui. Без аргументов в терминале tim открывает то же самое.
func (h *Home) Command() *cobra.Command {
	return &cobra.Command{
		Use:   "ui",
		Short: "Главный экран: поиск по проектам и командам, состояние машины",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return h.Run() },
	}
}

// Run открывает главный экран, а без конфига - мастер первого запуска; без терминала - заставка.
func (h *Home) Run() error {
	if !tui.Available() {
		h.overview()
		return nil
	}
	first := h.screen()
	if _, err := os.Stat(config.Path()); err != nil {
		first = projects.OnboardScreen(h.env, h.screen)
	}
	dir, err := tui.Run(first, h.options())
	if errors.Is(err, tui.ErrNoTTY) {
		h.overview()
		return nil
	}
	if err != nil || dir == "" {
		return err
	}
	// Шим читает папку из файла и делает cd; без шима подсказываем команду.
	if f := os.Getenv("TIM_CD_FILE"); f != "" {
		return os.WriteFile(f, []byte(dir), 0o600)
	}
	ui.Println(ui.Dim("перейти: cd ") + shellq.Quote(dir) + ui.Dim("  (tim init zsh - и tim сам сделает cd)"))
	return nil
}

func (h *Home) options() tui.Options {
	return tui.Options{Commands: h.commands, RunCommand: h.runLine}
}

// open: один экран на весь терминал; выбранную папку для cd отдаёт шиму.
func (h *Home) open(s tui.Screen) error {
	dir, err := tui.Run(s, h.options())
	if err != nil || dir == "" {
		return err
	}
	if f := os.Getenv("TIM_CD_FILE"); f != "" {
		return os.WriteFile(f, []byte(dir), 0o600)
	}
	return nil
}

// result: команда выполняется, не выходя из экрана, её вывод - на экране результата.
func (h *Home) result(args []string) tui.Action {
	title := strings.Join(args, " ")
	return tui.Action{Push: tui.NewResult(title, func() (string, error) {
		out, err := h.inside(args)
		if err == nil {
			if c, _, ferr := h.root.Find(args); ferr == nil {
				remember(c, args, h.utils)
			}
		}
		return out, err
	},
		tui.ResultOpts{Copy: runner.ToClipboard, CopyOutput: len(args) == 1 && runsBare[args[0]], Next: nextAfter(args)})}
}

// Remember: команда, набранная руками в терминале, - в недавнее главного экрана. Скрипты и агенты
// без терминала не записываются, чтобы не вытеснять то, что набирал человек.
func Remember(c *cobra.Command, args, utils []string) {
	if tui.Available() {
		remember(c, args, utils)
	}
}

// remember: у утилит и команд с чужими строками аргументы не пишутся - там бывают токены и пароли; длинные
// строки не пишутся вовсе; прогон с --dry-run и просмотр справки - не действия.
func remember(c *cobra.Command, args, utils []string) {
	if c == nil || !c.HasParent() || c.Hidden || runner.DryRun || c.Name() == "help" || slices.Contains(args, "-h") || slices.Contains(args, "--help") {
		return
	}
	top := c
	for top.Parent().HasParent() {
		top = top.Parent()
	}
	if notRemembered[top.Name()] {
		return
	}
	line := strings.Join(args, " ")
	if slices.Contains(utils, top.Name()) || argsHidden[top.Name()] {
		line = strings.TrimPrefix(c.CommandPath(), c.Root().Name()+" ")
	}
	if line != "" && len([]rune(line)) <= 80 {
		visits.RecordCommand(line, time.Now())
	}
}

// inside выполняет команду tim внутри экрана: вывод собирается, вопросы не задаются.
func (h *Home) inside(args []string) (string, error) {
	prevIn, prevDis := runner.NoInput, tui.Disabled
	runner.NoInput, tui.Disabled = true, true
	defer func() { runner.NoInput, tui.Disabled = prevIn, prevDis }()
	h.resetFlags(args)
	if h.prepare != nil {
		h.prepare(args)
	}
	var err error
	out := ui.Capture(func() {
		// Справку и ошибки cobra пишет сама, мимо ui: без этого они ложатся поверх экрана.
		h.root.SetOut(ui.Writer())
		h.root.SetErr(ui.Writer())
		h.root.SetArgs(args)
		err = h.root.Execute()
	})
	h.root.SetOut(nil)
	h.root.SetErr(nil)
	h.root.SetArgs(nil)
	return out, err
}

// after выполняет команду уже в терминале, после закрытия экрана; строка с "$" - чтобы было видно,
// что экран закрылся ради неё, а не упал.
func (h *Home) after(args []string) {
	ui.Println(ui.Dim("$ tim " + strings.Join(args, " ")))
	h.resetFlags(args)
	h.root.SetArgs(args)
	if err := h.root.Execute(); err != nil {
		ui.Println(ui.Bad(h.translate(err.Error())))
	}
}

// resetFlags: cobra помнит значения флагов между запусками в одном процессе; перед каждым - по умолчанию.
// Общие флаги (--dry-run, --no-input) заданы на весь запуск tim и остаются как были.
func (h *Home) resetFlags(args []string) {
	c, _, err := h.root.Find(args)
	if err != nil || c == nil {
		return
	}
	c.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Changed && h.root.PersistentFlags().Lookup(f.Name) == nil {
			_ = f.Value.Set(f.DefValue)
			f.Changed = false
		}
	})
}

// commands: подсказки для строки ":" - команды с описанием и примеры из справки.
// commands: подсказки для ":". После имени команды первыми идут её настоящие аргументы - из того же
// автодополнения, что в оболочке: проекты, алиасы, фоновые серверы, а не образцы из справки.
// commands: подсказки палитры; пустой ввод - сначала недавнее, иначе настоящие подстановки имён.
func (h *Home) commands(prefix string) []tui.CommandEntry {
	if strings.TrimSpace(prefix) == "" {
		return append(h.recentEntries(), h.entries()...)
	}
	return append(h.argEntries(prefix), h.entries()...)
}

// recentEntries: команды, которые недавно набирал, - новые первыми.
func (h *Home) recentEntries() []tui.CommandEntry {
	type item struct {
		line string
		last time.Time
	}
	var items []item
	for line, e := range visits.Commands() {
		items = append(items, item{line, e.Last})
	}
	slices.SortFunc(items, func(a, b item) int { return b.last.Compare(a.last) })
	var out []tui.CommandEntry
	for _, it := range items[:min(4, len(items))] {
		out = append(out, tui.CommandEntry{Line: it.line, Desc: ui.Short(time.Since(it.last)), Group: "недавнее"})
	}
	return out
}

func (h *Home) argEntries(prefix string) []tui.CommandEntry {
	fields := strings.Fields(prefix)
	toComplete := ""
	if len(fields) > 0 && !strings.HasSuffix(prefix, " ") {
		toComplete, fields = fields[len(fields)-1], fields[:len(fields)-1]
	}
	if len(fields) == 0 {
		return nil
	}
	c, rest, err := h.root.Find(fields)
	if err != nil || c == nil || c == h.root || c.ValidArgsFunction == nil {
		return nil
	}
	vals, _ := c.ValidArgsFunction(c, rest, toComplete)
	base := strings.Join(fields, " ")
	var out []tui.CommandEntry
	for _, v := range vals {
		name, desc, _ := strings.Cut(v, "\t")
		if strings.HasPrefix(name, toComplete) {
			out = append(out, tui.CommandEntry{Line: base + " " + name, Desc: desc, Group: "подстановка"})
		}
	}
	return out
}

func (h *Home) collectCommands() []tui.CommandEntry {
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
				// Команда с обязательным аргументом - шаблон: подставляется в строку для правки, а не запускается.
				out = append(out, tui.CommandEntry{Line: line, Desc: sub.Short, Template: strings.Contains(sub.Use, "<")})
			}
			walk(sub, line)
		}
	}
	walk(h.root, "")
	for _, ex := range h.examples {
		for _, l := range strings.Split(ex, "\n") {
			l = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), "tim "))
			desc := ""
			if i := strings.Index(l, "#"); i >= 0 {
				l, desc = strings.TrimSpace(l[:i]), strings.TrimSpace(l[i+1:])
			}
			// Конвейер оболочки (pbpaste | tim jwt) из строки ":" не выполнить - в подсказки не берём.
			if l != "" && !seen[l] && !strings.Contains(l, "|") {
				seen[l] = true
				out = append(out, tui.CommandEntry{Line: l, Desc: desc, Template: h.hasSample(l)})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Line < out[j].Line })
	return out
}

// hasSample: в примере есть значения для образца - проект shop, домен example.com, адрес. Такой
// пример - шаблон: его подставляют и правят, а не выполняют как есть. Флаги без значений не в счёт.
func (h *Home) hasSample(line string) bool {
	c, rest, err := h.root.Find(strings.Fields(line))
	if err != nil || c == nil {
		return true
	}
	for i := 0; i < len(rest); i++ {
		arg := rest[i]
		if !strings.HasPrefix(arg, "-") {
			return true
		}
		name := strings.TrimLeft(arg, "-")
		if strings.Contains(name, "=") {
			continue
		}
		f := c.Flags().Lookup(name)
		if f == nil {
			f = c.InheritedFlags().Lookup(name)
		}
		if f == nil && len(name) == 1 {
			f = c.Flags().ShorthandLookup(name)
		}
		if f != nil && f.NoOptDefVal == "" {
			i++ // значение флага
		}
	}
	return false
}
