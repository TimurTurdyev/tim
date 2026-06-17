package cmd

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/newproj"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var (
	newStack string
	newIn    string
	newNoGit bool
	newOpen  bool
)

// templateSteps: свой шаблон из конфига, иначе встроенный по стеку; port - нужен ли порт в реестре.
func templateSteps(stack string) ([]string, bool, error) {
	for _, t := range cfg.Templates {
		if t.Name == stack {
			return t.Steps, t.Port, nil
		}
	}
	steps, ok := newproj.Builtin(stack, runner.Has("uv"))
	if !ok {
		var own []string
		for _, t := range cfg.Templates {
			own = append(own, t.Name)
		}
		return nil, false, ui.Errorf("нет шаблона %s: встроенные %s; свои [[template]] в конфиге: %s",
			stack, strings.Join(newproj.Stacks(), " "), ui.Dash(strings.Join(own, " ")))
	}
	return steps, stack == "laravel" || stack == "php", nil
}

var newCmd = &cobra.Command{
	Use:   "new <имя> [--stack laravel|php|go|node|vite|python|empty|свой] [--in папка] [--no-git] [--open]",
	Short: "Создать проект по шаблону стека, git init и запись в реестр",
	Long: `Папка: <корень>/<in>/<имя>, а при заданном wrapper клон в <корень>/<in>/<имя>/<wrapper>.
Свои шаблоны - [[template]] в конфиге: name, steps (команды sh, подстановки {dir} {name} {parent}),
port = true, если проекту нужен порт для serve. Свой шаблон с именем встроенного его заменяет.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		if strings.ContainsAny(name, "/ \t") || strings.HasPrefix(name, ".") {
			return ui.Errorf("имя без пробелов, слешей и точки в начале: %s", name)
		}
		if _, ok := cfg.Project(name); ok {
			return ui.Errorf("проект %s уже есть в реестре", name)
		}
		steps, needPort, err := templateSteps(newStack)
		if err != nil {
			return err
		}
		dir, parent := newproj.Layout(cfg.Dev, newIn, name, cfg.Wrapper)
		if _, err := os.Stat(dir); err == nil {
			return ui.Errorf("папка уже есть: %s", dir)
		}
		ui.Println(ui.Label(name) + "  " + ui.Dim(newStack+" -> "+dir))
		if !runner.DryRun {
			if err := os.MkdirAll(parent, 0o755); err != nil {
				return err
			}
		} else {
			ui.Println(ui.Dim("$ mkdir -p " + parent))
		}
		for _, step := range steps {
			if err := runner.ExecIn(parent, nil, "sh", "-c", newproj.Expand(step, dir, name, parent)); err != nil {
				return ui.Errorf("шаг не прошёл: %s: %v; созданное осталось в %s", step, err, parent)
			}
		}
		if !newNoGit {
			if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
				if err := runner.ExecIn(dir, nil, "git", "init", "-q"); err != nil {
					return ui.Errorf("git init: %v", err)
				}
			}
		}
		p := config.Project{Name: name, Path: cfg.Relative(dir)}
		if needPort {
			var used []int
			for _, q := range cfg.Projects {
				if q.Port > 0 {
					used = append(used, q.Port)
				}
			}
			p.Port = newproj.NextPort(used, netinfo.DefaultPort, netinfo.BusyPorts().Busy)
		}
		if err := cfg.AddProject(p); err != nil {
			return err
		}
		if err := saveConfig(cfg); err != nil {
			return err
		}
		line := ui.Label("готово ") + name + ui.Dim("  "+p.Path)
		if p.Port > 0 {
			line += ui.Dim(", порт " + strconv.Itoa(p.Port))
		}
		ui.Println(line)
		ui.Println(ui.Dim("дальше: tim go " + name + ", tim serve " + name))
		if newOpen {
			return openCmd.RunE(openCmd, []string{name})
		}
		return nil
	},
}

func init() {
	newCmd.Flags().StringVar(&newStack, "stack", "empty", "шаблон: "+strings.Join(newproj.Stacks(), ", ")+" или свой из конфига")
	newCmd.Flags().StringVar(&newIn, "in", "projects", "папка внутри корня проектов")
	newCmd.Flags().BoolVar(&newNoGit, "no-git", false, "без git init")
	newCmd.Flags().BoolVar(&newOpen, "open", false, "открыть в редакторе после создания")
	_ = newCmd.RegisterFlagCompletionFunc("stack", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		names := newproj.Stacks()
		for _, t := range cfg.Templates {
			names = append(names, t.Name)
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	})
	root.AddCommand(newCmd)
}
