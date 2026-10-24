package cmd

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/bypass"
	"github.com/TimurTurdyev/tim/internal/routewatch"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var routeStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Как работает страж маршрутов: последняя проверка, починки и их причины",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		installed, running := bypass.GuardInstalled(), bypass.GuardRunning()
		switch {
		case !installed:
			ui.Println(ui.Warn("служба не установлена") + ui.Dim("  sudo tim route install"))
		case running:
			ui.Println(ui.Label("служба работает") + ui.Dim("  журнал "+bypass.GuardLog()))
		default:
			ui.Println(ui.Bad("служба установлена, но не работает") + ui.Dim("  журнал "+bypass.GuardLog()))
		}
		st, ok := bypass.GuardStatus()
		if !ok {
			ui.Println(ui.Dim("отчёта стража нет"))
			return nil
		}
		now := time.Now()
		ui.Println(ui.Label("последняя проверка ") + ui.Duration(now.Sub(st.LastCheck)) + " назад" +
			ui.Dim(", запущен "+st.Started.Format("02.01 15:04")))
		ui.Println(ui.Label("сеть               ") + st.Iface + " шлюз " + ui.Dash(st.Gateway))
		ok2 := strconv.Itoa(st.OK) + " из " + strconv.Itoa(st.Want)
		if st.OK < st.Want {
			ok2 = ui.Warn(ok2)
		}
		ui.Println(ui.Label("маршрутов в порядке ") + ok2)
		if st.Error != "" {
			ui.Println(ui.Bad("ошибка: " + st.Error))
		}
		day, week := st.Since(now.Add(-24*time.Hour)), st.Since(now.AddDate(0, 0, -7))
		ui.Println(ui.Label("починок            ") + fmt.Sprintf("за сутки %d, за неделю %d", day, week))
		if len(st.Repairs) == 0 {
			return nil
		}
		var recent []routewatch.Repair
		for _, r := range st.Repairs {
			if r.At.After(now.AddDate(0, 0, -7)) {
				recent = append(recent, r)
			}
		}
		sum := routewatch.Summary(recent)
		kinds := make([]string, 0, len(sum))
		for k := range sum {
			kinds = append(kinds, k)
		}
		sort.Slice(kinds, func(i, j int) bool { return sum[kinds[i]] > sum[kinds[j]] })
		for _, k := range kinds {
			ui.Println(ui.Dim("  " + ui.Pad(strconv.Itoa(sum[k]), 5) + k))
		}
		ui.Println(ui.Dim("последние:"))
		from := max(0, len(st.Repairs)-5)
		for _, r := range st.Repairs[from:] {
			ui.Println(ui.Dim("  " + r.At.Format("02.01 15:04") + "  " + ui.Pad(r.Dest, 18) + " " + r.Reason))
		}
		return nil
	},
}

func init() {
	routeCmd.AddCommand(routeStatusCmd)
}
