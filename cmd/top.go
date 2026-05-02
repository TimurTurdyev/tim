package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/machine"
	"github.com/TimurTurdyev/tim/internal/procs"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var (
	topBy  string
	topN   int
	topCPU bool
)

// projectOfPID: проект по рабочей папке процесса; приложения из .app в проекты не записываем -
// они стартуют в той папке, откуда их открыли.
func projectOfPID() func(procs.Sample) string {
	cwd := procs.Cwds(nil)
	return func(s procs.Sample) string {
		dir := cwd[s.PID]
		if dir == "" || dir == "/" || strings.Contains(s.Comm, ".app/") {
			return ""
		}
		if p, ok := cfg.ProjectHere(dir); ok {
			return p.Name
		}
		return ""
	}
}

var topCmd = &cobra.Command{
	Use:   "top [--by app|project] [--cpu] [-n 10]",
	Short: "Кто занял память и CPU: по приложениям или по проектам",
	Long: `Складывает процессы по приложению (все помощники браузера - в одну строку) или по проекту
реестра (по рабочей папке процесса). Память - RSS: общие страницы считаются в каждом процессе,
поэтому у многопроцессных приложений сумма выше, чем в Мониторе системы.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if tui.Available() && topBy == "app" {
			return runScreen(procsScreen(""))
		}
		full, err := machine.All()
		if err != nil {
			return err
		}
		list := make([]procs.Sample, len(full))
		for i, p := range full {
			list[i] = p.Sample
		}
		key := func(s procs.Sample) string { return procs.Name(s.Comm, s.Args) }
		label := "приложение"
		switch topBy {
		case "app":
		case "project":
			key, label = projectOfPID(), "проект"
		default:
			return ui.Errorf("--by app или project, а не %s", topBy)
		}
		groups := procs.GroupBy(list, key, topCPU)
		if len(groups) == 0 {
			ui.Println(ui.Dim("процессов проектов не нашлось"))
			return nil
		}
		var total int64
		for _, g := range groups {
			total += g.RSS
		}
		ui.Println(ui.Dim(ui.Pad(label, 28) + ui.Pad("память", 10) + ui.Pad("CPU", 8) + "процессов"))
		for i, g := range groups {
			if i == topN {
				ui.Println(ui.Dim(fmt.Sprintf("и ещё %d", len(groups)-topN)))
				break
			}
			ui.Println(ui.Label(ui.Pad(ui.Clip(g.Name, 27), 28)) + ui.Pad(ui.Bytes(g.RSS), 10) +
				ui.Pad(strconv.FormatFloat(g.CPU, 'f', 1, 64)+"%", 8) + ui.Dim(strconv.Itoa(g.Count)))
		}
		if topBy == "project" {
			ui.Println(ui.Dim("всего у проектов " + ui.Bytes(total)))
		}
		return nil
	},
}

func init() {
	topCmd.Flags().StringVar(&topBy, "by", "app", "app или project")
	topCmd.Flags().IntVarP(&topN, "count", "n", 10, "сколько строк")
	topCmd.Flags().BoolVar(&topCPU, "cpu", false, "сортировать по CPU, а не по памяти")
	root.AddCommand(topCmd)
}
