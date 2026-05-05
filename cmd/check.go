package cmd

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/machine"
	"github.com/TimurTurdyev/tim/internal/sites"
	"github.com/TimurTurdyev/tim/internal/sysinfo"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var (
	checkLocal bool
	checkNet   bool
	checkHours float64
)

// checkProjects: проекты для обхода. Без аргументов - весь реестр.
func checkProjects(args []string) ([]*config.Project, error) {
	if len(args) == 0 {
		out := make([]*config.Project, 0, len(cfg.Projects))
		for i := range cfg.Projects {
			out = append(out, &cfg.Projects[i])
		}
		return out, nil
	}
	var out []*config.Project
	for _, name := range args {
		p, err := cfg.Pick(name)
		if err != nil {
			// Домен, которого нет в реестре, проверяем как есть: сайт, сертификат, почта, регистрация.
			if strings.Contains(name, ".") && !strings.Contains(name, "/") {
				out = append(out, &config.Project{Name: name, URL: "https://" + strings.TrimPrefix(strings.TrimPrefix(name, "https://"), "http://")})
				continue
			}
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

var checkCols = []struct {
	name  string
	width int
}{
	{"git", 26},
	{"сайт", 12},
	{"сертификат", 14},
	{"почта", 8},
	{"домен", 10},
}

func checkPaint(c health.Check, text string) string {
	switch c.Level {
	case health.Bad:
		return ui.Bad(text)
	case health.Warn:
		return ui.Warn(text)
	case health.Skip:
		return ui.Dim(text)
	}
	return text
}

func printCheck(rows []health.Row) {
	// Колонку, в которой нечего показать ни одному проекту, не печатаем.
	cols := checkCols[:0:0]
	for _, col := range checkCols {
		for _, r := range rows {
			if r.Get(col.name).Level != health.Skip {
				cols = append(cols, col)
				break
			}
		}
	}
	head := ui.Pad("проект", 22)
	for _, col := range cols {
		head += ui.Pad(col.name, col.width)
	}
	ui.Println(ui.Dim(head))
	for _, r := range rows {
		line := ui.Pad(r.Name, 22)
		for _, col := range cols {
			c := r.Get(col.name)
			text := ui.Clip(c.Text, col.width-1)
			line += checkPaint(c, text) + strings.Repeat(" ", col.width-len([]rune(text)))
		}
		ui.Println(strings.TrimRight(line, " "))
		for _, c := range r.Problems() {
			ui.Println("  " + ui.Dim(c.Detail))
		}
	}
}

// checkMachine: замечания не о проектах, а о самой машине.
func checkMachine() []string {
	out := machine.HogLines("")
	limit := time.Duration(checkHours * float64(time.Hour))
	var forgotten []string
	for _, r := range listenRows() {
		if r.project != "" && !strings.HasSuffix(r.project, "?") && r.Uptime >= limit {
			forgotten = append(forgotten, fmt.Sprintf("%s:%s", r.project, r.Port))
		}
	}
	if len(forgotten) > 0 {
		out = append(out, ui.Warn(fmt.Sprintf("dev-серверы старше %g ч: ", checkHours))+strings.Join(forgotten, " ")+ui.Dim("  (tim ports --old --kill)"))
	}
	if d, ok := sysinfo.DataDisk(); ok && d.Percent() >= 85 {
		out = append(out, ui.Warn(fmt.Sprintf("диск занят на %d%%", d.Percent()))+ui.Dim("  (tim disk)"))
	}
	return out
}

var checkCmd = &cobra.Command{
	Use:   "check [проект...]",
	Short: "Обход хозяйства: git, сайт, сертификат, почта, срок домена по всем проектам",
	RunE: func(cmd *cobra.Command, args []string) error {
		switch {
		case checkInstall:
			return installCheck()
		case checkUninstall:
			return uninstallCheck()
		}
		if len(args) == 0 && !checkNotify && tui.Available() {
			return runScreen(checkScreen())
		}
		projects, err := checkProjects(args)
		if err != nil {
			return err
		}
		if len(projects) == 0 {
			return ui.Errorf("в реестре нет проектов: tim proj scan")
		}
		rows := make([]health.Row, len(projects))
		sem := make(chan struct{}, 8)
		var wg sync.WaitGroup
		for i, p := range projects {
			wg.Go(func() {
				sem <- struct{}{}
				defer func() { <-sem }()
				rows[i] = sites.CheckRow(p, cfg.ProjectDir(p), checkLocal, checkNet)
			})
		}
		wg.Wait()
		// Проекты, по которым проверить было нечего, в таблице только шумят.
		kept := rows[:0]
		for _, r := range rows {
			if r.Level() != health.Skip {
				kept = append(kept, r)
			}
		}
		rows = kept
		if len(rows) == 0 {
			return ui.Errorf("проверять нечего: ни у одного проекта нет git и адреса сайта (tim sync --remote)")
		}
		health.Sort(rows)
		printCheck(rows)
		if checkNotify {
			notifyProblems(rows)
		}
		ok, warn, bad := health.Counts(rows)
		ui.Println(ui.Dim(fmt.Sprintf("всего %d: ок %d, внимание %d", len(rows), ok, warn)))
		if lines := checkMachine(); len(lines) > 0 {
			ui.Println("")
			for _, l := range lines {
				ui.Println(l)
			}
		}
		if bad > 0 {
			return ui.Errorf("требуют внимания: %d из %d", bad, len(rows))
		}
		return nil
	},
}

func init() {
	checkCmd.Flags().BoolVar(&checkLocal, "local", false, "только локальные проверки, без сети")
	checkCmd.Flags().BoolVar(&checkNet, "net", false, "только сетевые проверки")
	checkCmd.Flags().Float64Var(&checkHours, "hours", 8, "с какого возраста dev-сервер считается забытым")
	checkCmd.Flags().BoolVar(&checkInstall, "install", false, "обходить каждый день и присылать уведомление, если что-то плохо")
	checkCmd.Flags().BoolVar(&checkUninstall, "uninstall", false, "убрать ежедневный обход")
	checkCmd.Flags().BoolVar(&checkNotify, "notify", false, "уведомление системы, если есть плохие строки")
	checkCmd.Flags().StringVar(&checkAt, "at", "09:30", "во сколько обходить (с --install)")
	root.AddCommand(checkCmd)
}
