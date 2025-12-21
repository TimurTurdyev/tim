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

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var cfg *config.Config

var root = &cobra.Command{
	Use:   "tim",
	Short: "Помощник в терминале: проекты, сеть, туннели, запуск, утилиты",
	Long: `tim держит под рукой проекты из реестра, ssh и маршруты мимо VPN, туннели, локальный запуск,
сводку по машине и мелкие утилиты разработчика. Конфиг: ~/.config/tim/config.toml (TIM_CONFIG).
TIM_DRY_RUN=1 или --dry-run печатает команды, меняющие систему, вместо запуска.`,
	SilenceUsage: true,
	Args:         cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		overview()
		return nil
	},
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		tui.Disabled = tui.Disabled || runner.NoInput
		var err error
		cfg, err = config.Load()
		if err != nil {
			return ui.Errorf("конфиг %s: %v", config.Path(), err)
		}
		return nil
	},
}

func init() {
	root.PersistentFlags().BoolVar(&runner.DryRun, "dry-run", runner.DryRun, "показать команды, ничего не выполняя")
	root.PersistentFlags().BoolVar(&runner.NoInput, "no-input", runner.NoInput, "ни о чём не спрашивать: для скриптов и cron")
	root.CompletionOptions.HiddenDefaultCmd = true
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

var groups = []struct {
	id, title string
	cmds      []string
}{
	{"proj", "Проекты:", []string{"proj", "new", "go", "db", "each", "sync", "motd", "disk", "config", "init"}},
	{"net", "Сервер и сеть:", []string{"ssh", "tunnel", "route", "vpn", "net", "ip", "wifi", "ports", "port", "stale", "dns", "cert", "ssl", "site", "domain", "mail", "http"}},
	{"run", "Локальный запуск:", []string{"serve", "env", "logs", "open", "svc", "php"}},
	{"util", "Утилиты:", []string{"hex", "dec", "b64", "jwt", "ts", "uuid", "hash", "json", "url", "pass", "qr", "cron", "doctor", "version", "self-update"}},
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

func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

// Root: корневая команда с группами и справкой, для генератора man.
func Root() *cobra.Command {
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
	return fmt.Errorf("%w\n\nDid you mean this?\n\t--%s\n", err, best)
}

func Execute() error {
	assignGroups()
	root.SetFlagErrorFunc(flagSuggestion)
	if needsHelp(os.Args[1:]) {
		applyHelp()
	}
	if err := root.Execute(); err != nil {
		if errors.Is(err, errPickCanceled) {
			return nil
		}
		msg := err.Error()
		unknownCmd := regexp.MustCompile(`^unknown command "([^" ]+)" for "tim"`)
		if m := unknownCmd.FindStringSubmatch(msg); m != nil && !strings.Contains(msg, "Did you mean") {
			root.SuggestionsMinimumDistance = 2
			if sugg := bestSuggestions(m[1], root.SuggestionsFor(m[1]), 3); len(sugg) > 0 {
				msg += "\n\nDid you mean this?\n\t" + strings.Join(sugg, "\n\t") + "\n"
			}
		}
		ui.Println(ui.Bad(translateError(msg)))
		return err
	}
	return nil
}

func projectNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	c, err := config.Load()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	var names []string
	for _, p := range c.Projects {
		names = append(names, p.Name)
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}
