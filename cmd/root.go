// Package cmd: команды tim на cobra.
package cmd

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/bypass"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/disk"
	"github.com/TimurTurdyev/tim/internal/home"
	"github.com/TimurTurdyev/tim/internal/local"
	"github.com/TimurTurdyev/tim/internal/machine"
	"github.com/TimurTurdyev/tim/internal/motd"
	"github.com/TimurTurdyev/tim/internal/projects"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/serve"
	"github.com/TimurTurdyev/tim/internal/servers"
	"github.com/TimurTurdyev/tim/internal/shell"
	"github.com/TimurTurdyev/tim/internal/sites"
	"github.com/TimurTurdyev/tim/internal/tools"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// env: окружение команд разделов, конфиг в нём появляется перед запуском команды.
var env = &app.Env{}

// screens: экранный режим - главный экран и строка ":" поверх тех же команд.
var screens *home.Home

var root = &cobra.Command{
	Use:   "tim",
	Short: "Помощник в терминале: проекты, сеть, туннели, запуск, утилиты",
	Long: `tim держит под рукой проекты из реестра, ssh и маршруты мимо VPN, туннели, локальный запуск,
сводку по машине и мелкие утилиты разработчика. Конфиг: ~/.config/tim/config.toml (TIM_CONFIG).
TIM_DRY_RUN=1 или --dry-run печатает команды, меняющие систему, вместо запуска.`,
	SilenceUsage:  true,
	Args:          cobra.NoArgs,
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		tui.Disabled = tui.Disabled || runner.NoInput
		c, err := config.Load()
		if err != nil {
			return ui.Errorf("конфиг %s: %v", config.Path(), err)
		}
		env.Cfg = c
		return nil
	},
}

var groups = []struct {
	id, title string
	cmds      []string
}{
	{"proj", "Проекты:", []string{"proj", "new", "go", "db", "each", "sync", "config", "init"}},
	{"machine", "Машина:", []string{"status", "hw", "top", "ps", "kill", "disk", "stats"}},
	{"net", "Сервер и сеть:", []string{"ssh", "server", "servers", "tunnel", "direct", "route", "vpn", "net", "ip", "ports", "port", "dns", "cert", "site", "audit", "domain", "mail", "http"}},
	{"run", "Локальный запуск:", []string{"serve", "env", "logs", "open", "svc", "php"}},
	{"util", "Утилиты:", utilCmds},
	{"self", "Сам tim:", []string{"doctor", "version", "self-update"}},
}

// utilCmds: мелкие утилиты. В общей справке они одной строкой - полный список: tim help utils.
var utilCmds = []string{"hex", "dec", "b64", "jwt", "ts", "uuid", "hash", "json", "url", "pass", "qr", "cron"}

func init() {
	root.PersistentFlags().BoolVar(&runner.DryRun, "dry-run", runner.DryRun, "показать команды, ничего не выполняя")
	root.PersistentFlags().BoolVar(&runner.NoInput, "no-input", runner.NoInput, "ни о чём не спрашивать: для скриптов и cron")
	root.PersistentFlags().BoolVarP(&runner.Yes, "yes", "y", false, "на все вопросы отвечать да: для скриптов и агентов")
	root.CompletionOptions.HiddenDefaultCmd = true
	env.Version = resolveVersion()
	screens = home.New(env, root, home.Options{Examples: examples, Overview: overview, Translate: translateError,
		Utils: utilCmds, Prepare: func(args []string) {
			if needsHelp(args) {
				applyHelp()
			}
		}})
	// В терминале tim без аргументов - главный экран; в трубе и скриптах - заставка.
	root.RunE = func(cmd *cobra.Command, args []string) error { return screens.Run() }
	runner.ConfirmFunc = func(q string, def bool) (bool, bool) {
		if !tui.Available() {
			return false, false
		}
		yes, err := tui.Confirm(q, def)
		if err != nil && !errors.Is(err, tui.ErrCanceled) {
			return false, false
		}
		return yes, true
	}
}

// sections: команды разделов. Корень только собирает их; подкоманды с пометкой app.Parent
// встают под команду другого раздела.
func sectionCommands() [][]*cobra.Command {
	return [][]*cobra.Command{
		tools.Commands(),
		local.Commands(env),
		shell.Commands(env),
		machine.Commands(env),
		disk.Commands(env),
		bypass.Commands(env),
		serve.Commands(env),
		servers.Commands(env),
		sites.Commands(env),
		projects.Commands(env),
		motd.Commands(env),
		{screens.Command()},
	}
}

func mount() {
	var children []*cobra.Command
	for _, list := range sectionCommands() {
		for _, c := range list {
			if c.Annotations[app.Parent] != "" {
				children = append(children, c)
				continue
			}
			root.AddCommand(c)
		}
	}
	for _, c := range children {
		parent, _, err := root.Find([]string{c.Annotations[app.Parent]})
		if err != nil {
			parent = root
		}
		parent.AddCommand(c)
	}
}

// assignGroups раскладывает команды по разделам справки; вызывается после всех init().
func assignGroups() {
	byName := map[string]*cobra.Command{}
	for _, c := range root.Commands() {
		byName[c.Name()] = c
	}
	for _, g := range groups {
		root.AddGroup(&cobra.Group{ID: g.id, Title: g.title})
		for _, n := range g.cmds {
			if c, ok := byName[n]; ok {
				c.GroupID = g.id
			}
		}
	}
}

// needsHelp: справка будет показана - флаг справки, help, man или группа команд без своего действия.
// Шаблон и примеры раскладываются только тогда, чтобы не тратить на это старт обычных команд.
func needsHelp(args []string) bool {
	for _, a := range args {
		if a == "-h" || a == "--help" || a == "help" {
			return true
		}
		if a == "--" {
			break
		}
	}
	c, _, err := root.Find(args)
	return err == nil && c != root && !c.Runnable()
}

// bestSuggestions: сначала начинающиеся с набранного, затем по расстоянию редактирования; не больше n.
func bestSuggestions(typed string, list []string, n int) []string {
	sort.SliceStable(list, func(i, j int) bool {
		pi, pj := strings.HasPrefix(list[i], typed), strings.HasPrefix(list[j], typed)
		if pi != pj {
			return pi
		}
		return editDistance(typed, list[i]) < editDistance(typed, list[j])
	})
	return list[:min(n, len(list))]
}

// editDistance: правки по одной букве; перестановка соседних - одна правка, иначе modt далеко от motd.
func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	d := make([][]int, len(ra)+1)
	for i := range d {
		d[i] = make([]int, len(rb)+1)
		d[i][0] = i
	}
	for j := range d[0] {
		d[0][j] = j
	}
	for i := 1; i <= len(ra); i++ {
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			d[i][j] = min(d[i-1][j]+1, d[i][j-1]+1, d[i-1][j-1]+cost)
			if i > 1 && j > 1 && ra[i-1] == rb[j-2] && ra[i-2] == rb[j-1] {
				d[i][j] = min(d[i][j], d[i-2][j-2]+1)
			}
		}
	}
	return d[len(ra)][len(rb)]
}

// Root: корневая команда с группами и справкой, для генератора man.
func Root() *cobra.Command {
	mount()
	assignGroups()
	applyHelp()
	root.DisableAutoGenTag = true
	return root
}

func Version() string { return resolveVersion() }

// flagSuggestion: на неизвестный флаг - ближайший флаг этой команды. Частая ошибка - лишний
// знак, скопированный вместе с флагом: --leftovers: вместо --leftovers.
func flagSuggestion(c *cobra.Command, err error) error {
	m := regexp.MustCompile(`^unknown flag: --(\S+)`).FindStringSubmatch(err.Error())
	if m == nil {
		return err
	}
	typed := strings.TrimRight(m[1], ":;,.")
	var names []string
	c.Flags().VisitAll(func(f *pflag.Flag) { names = append(names, f.Name) })
	c.InheritedFlags().VisitAll(func(f *pflag.Flag) { names = append(names, f.Name) })
	if len(names) == 0 {
		return err
	}
	best := bestSuggestions(typed, names, 1)[0]
	if best != typed && editDistance(typed, best) > max(2, len(typed)/3) {
		return err
	}
	return fmt.Errorf("%w\n\nDid you mean this?\n\t--%s", err, best)
}

func Execute() error {
	mount()
	assignGroups()
	root.SetFlagErrorFunc(flagSuggestion)
	if needsHelp(os.Args[1:]) {
		applyHelp()
	}
	c, err := root.ExecuteC()
	if err != nil {
		if errors.Is(err, app.ErrCanceled) {
			return nil
		}
		msg := err.Error()
		if strings.HasPrefix(msg, `unknown command "`) && strings.HasSuffix(strings.SplitN(msg, "\n", 2)[0], `for "tim"`) {
			if hint := unknownHint(os.Args[1:]); hint != "" {
				ui.Println(ui.Bad(hint))
				return err
			}
		}
		ui.Println(ui.Bad(translateError(msg)))
		return err
	}
	home.Remember(c, os.Args[1:], utilCmds)
	return nil
}
