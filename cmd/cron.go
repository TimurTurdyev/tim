package cmd

import (
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/cronx"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Полублоки: верхняя половина, нижняя, обе. Записаны кодами, чтобы исходник был набран с клавиатуры.
var cronRuns int

var cronCmd = &cobra.Command{
	Use:   `cron "<выражение>" [-n 5]`,
	Short: "Что значит cron-выражение и когда ближайшие запуски",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		expr := strings.Join(args, " ")
		s, err := cronx.Parse(expr)
		if err != nil {
			return ui.Errorf("%s: %v", expr, err)
		}
		ui.Println(ui.Label(s.Describe()))
		for _, t := range s.Next(time.Now(), cronRuns) {
			ui.Println("  " + t.Format("Mon 2006-01-02 15:04") + ui.Dim("  "+ago(t)))
		}
		return nil
	},
}
