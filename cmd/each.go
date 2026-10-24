package cmd

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/each"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/shellq"
	"github.com/TimurTurdyev/tim/internal/ui"
)

type eachTarget struct {
	dir, name, version string
}

// eachTargets: папки проектов из реестра (и над клоном, если задан wrapper) с маркером, а со scan - всё под корнем.
func eachTargets(t *config.Task, scan bool) []eachTarget {
	names := map[string]string{}
	var candidates []string
	for i := range cfg.Projects {
		p := &cfg.Projects[i]
		dir := filepath.Clean(cfg.ProjectDir(p))
		candidates = append(candidates, dir)
		names[dir] = p.Name
		if cfg.Wrapper != "" && filepath.Base(dir) == cfg.Wrapper {
			candidates = append(candidates, filepath.Dir(dir))
			names[filepath.Dir(dir)] = p.Name
		}
	}
	if scan || t.Scan {
		candidates = append(candidates, each.Scan(cfg.Dev, t.Marker, 4)...)
	}
	var out []eachTarget
	for _, d := range each.Locate(candidates, t.Marker) {
		name := names[d]
		if name == "" {
			name = cfg.Relative(d)
		}
		e := eachTarget{dir: d, name: name}
		if t.VersionKey != "" {
			e.version = each.JSONVersion(filepath.Join(d, t.Marker), t.VersionKey)
		}
		out = append(out, e)
	}
	return out
}

// latestVersion: что печатает version_cmd; пусто - сравнивать не с чем.
func latestVersion(t *config.Task) string {
	if t.VersionCmd == "" || t.VersionKey == "" {
		return ""
	}
	out, err := runner.Output("sh", "-c", t.VersionCmd)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

var (
	eachList    bool
	eachAll     bool
	eachScan    bool
	eachMarker  string
	eachVerbose bool
)

var eachCmd = &cobra.Command{
	Use:   "each [задача] [проект...] [--list] [--all] [--scan] | each --marker файл -- команда",
	Short: "Выполнить команду во всех проектах, где есть файл-маркер; задачи - [[task]] в конфиге",
	Long: `Задача в конфиге:

  [[task]]
  name = "deps"
  marker = "composer.json"      # где выполнять
  run = "composer outdated --direct"
  version_key = "version"       # необязательно: ключ версии в JSON-маркере
  version_cmd = "tool --version" # и с чем сравнивать; тогда по умолчанию только устаревшие

Без задачи - список задач. Разово: tim each --marker go.mod -- go mod tidy.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		var t *config.Task
		var names []string
		dash := cmd.ArgsLenAtDash()
		switch {
		case eachMarker != "":
			if dash < 0 || dash == len(args) {
				return ui.Errorf("команду пиши после --: tim each --marker go.mod -- go mod tidy")
			}
			t = &config.Task{Name: "разово", Marker: eachMarker, Run: shellq.Join(args[dash:])}
			names = args[:dash]
		case len(args) == 0:
			if len(cfg.Tasks) == 0 {
				ui.Println(ui.Dim("задач нет: добавь [[task]] в конфиг (tim each --help) или tim each --marker файл -- команда"))
				return nil
			}
			for _, t := range cfg.Tasks {
				ui.Println(ui.Label(ui.Pad(t.Name, 14)) + " " + ui.Pad(t.Marker, 22) + " " + ui.Dim(t.Run))
			}
			return nil
		default:
			found, ok := cfg.Task(args[0])
			if !ok {
				return ui.Errorf("нет задачи %s: tim each", args[0])
			}
			t, names = found, args[1:]
		}
		latest := latestVersion(t)
		targets := eachTargets(t, eachScan)
		if eachList {
			old := 0
			for _, e := range targets {
				line := ui.Pad(e.name, 26)
				if t.VersionKey != "" {
					state := ui.Dim("свежая")
					if latest != "" && each.Older(e.version, latest) {
						state = ui.Warn("устарела")
						old++
					}
					line += " " + ui.Pad(ui.Dash(e.version), 8) + " " + state
				}
				ui.Println(line)
			}
			summary := t.Marker + ": " + strconv.Itoa(len(targets))
			if latest != "" {
				summary += ", устарело " + strconv.Itoa(old) + " (последняя " + latest + ")"
			}
			ui.Println(ui.Dim(summary))
			return nil
		}
		want := map[string]bool{}
		for _, n := range names {
			want[n] = true
		}
		var todo []eachTarget
		for _, e := range targets {
			switch {
			case len(want) > 0:
				if want[e.name] {
					todo = append(todo, e)
				}
			case eachAll || latest == "" || each.Older(e.version, latest):
				todo = append(todo, e)
			}
		}
		if len(todo) == 0 {
			ui.Println(ui.Dim("делать нечего"))
			return nil
		}
		failed := 0
		for _, e := range todo {
			out, err := runner.CaptureIn(e.dir, "sh", "-c", t.Run)
			if err != nil {
				failed++
				ui.Println(ui.Bad(ui.Pad(e.name, 26)) + " " + err.Error())
				ui.Println(ui.Dim(tail(out, 5)))
				continue
			}
			if runner.DryRun {
				continue
			}
			line := ui.Label(ui.Pad(e.name, 26)) + " готово"
			if t.VersionKey != "" {
				line = ui.Label(ui.Pad(e.name, 26)) + " " + ui.Dash(e.version) + " -> " + each.JSONVersion(filepath.Join(e.dir, t.Marker), t.VersionKey)
			}
			ui.Println(line)
			if eachVerbose && strings.TrimSpace(out) != "" {
				ui.Println(ui.Dim(strings.TrimRight(out, "\n")))
			}
		}
		if failed > 0 {
			return ui.Errorf("не вышло: %d из %d", failed, len(todo))
		}
		return nil
	},
}

func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func init() {
	f := eachCmd.Flags()
	f.BoolVar(&eachList, "list", false, "только показать, где и в каких версиях")
	f.BoolVar(&eachAll, "all", false, "во всех, даже свежих")
	f.BoolVar(&eachScan, "scan", false, "искать и вне реестра, под корнем проектов")
	f.StringVar(&eachMarker, "marker", "", "разовый запуск: файл-маркер, команда после --")
	f.BoolVarP(&eachVerbose, "verbose", "v", false, "печатать вывод команды")
	root.AddCommand(eachCmd)
}
