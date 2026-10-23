package cmd

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/brand"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// version задаётся при сборке: -ldflags "-X github.com/TimurTurdyev/tim/cmd.version=v0.1.0".
var version = ""

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Версия tim",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ui.Print(versionText())
		return nil
	},
}

func resolveVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

func init() {
	root.Version = resolveVersion()
	cobra.AddTemplateFunc("versionText", versionText)
	root.SetVersionTemplate("{{versionText}}")
	root.AddCommand(versionCmd)
}

// versionText: --version и version одинаково. В терминале - лок-ап, в трубе - одна строка для
// скриптов: "tim e3642b7 (go1.26.5 darwin/arm64)".
func versionText() string {
	if on, _ := ui.Colors(); !on {
		return fmt.Sprintf("%s %s (%s %s/%s)\n", brand.Name, strings.TrimPrefix(env.Build(), "сборка "),
			runtime.Version(), runtime.GOOS, runtime.GOARCH)
	}
	return strings.Join(lockup(versionLines()), "\n") + "\n"
}

// versionLines: имя и сборка; движок и дата сборки; конфиг и реестр.
func versionLines() [][]string {
	engine := []string{ui.Dim(runtime.Version() + " " + runtime.GOOS + "/" + runtime.GOARCH)}
	if t, ok := buildTime(); ok {
		engine = append(engine, ui.Dim(fmt.Sprintf("сборка %s", ui.DateLong(t))))
	}
	place := []string{ui.Dim(paths.Show(config.Path()))}
	// --version срабатывает раньше, чем корень читает конфиг.
	cfg := env.Cfg
	if cfg == nil {
		cfg, _ = config.Load()
	}
	if cfg != nil {
		place = append(place, ui.Dim(ui.Count(len(cfg.Projects), "проект", "проекта", "проектов")))
	}
	place = append(place, ui.Dim("обновить: tim self-update"))
	return [][]string{{ui.Strong(brand.Name) + "  " + ui.Dim(env.Build())}, engine, place}
}

// lockup: лок-ап в тексте - значок с колонки 2, строки с 15-й.
func lockup(lines [][]string) []string {
	_, depth := ui.Colors()
	look := brand.Look{Mood: brand.Calm, Light: ui.Light()}
	return ui.Header(lines, ui.HeaderOpts{Look: look, Indent: 1, Width: ui.Cols(), Sprite: depth != brand.Basic})
}

// buildTime: когда собран бинарник - по метке коммита из сборки go.
func buildTime() (time.Time, bool) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return time.Time{}, false
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.time" {
			t, err := time.Parse(time.RFC3339, s.Value)
			return t, err == nil
		}
	}
	return time.Time{}, false
}
