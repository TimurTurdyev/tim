package cmd

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"

	"github.com/TimurTurdyev/tim/internal/ui"
)

var manCmd = &cobra.Command{
	Use:    "man <папка>",
	Short:  "Сгенерировать man-страницы всех команд",
	Hidden: true,
	Args:   cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := os.MkdirAll(args[0], 0o755); err != nil {
			return err
		}
		root.DisableAutoGenTag = true
		header := &doc.GenManHeader{Title: "TIM", Section: "1", Source: "tim " + resolveVersion(), Manual: "tim"}
		if err := doc.GenManTree(root, header, args[0]); err != nil {
			return ui.Errorf("man: %v", err)
		}
		return nil
	},
}

func init() { root.AddCommand(manCmd) }
