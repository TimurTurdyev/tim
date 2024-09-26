package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var hookCmd = &cobra.Command{
	Use:    "hook",
	Hidden: true,
	// Хук не должен ронять оболочку из-за сломанного конфига: тогда просто молчит.
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		c, err := config.Load()
		if err != nil {
			c = config.Default()
		}
		cfg = c
		return nil
	},
}

var (
	hookLast  string
	hookQuiet bool
)

var hookCdCmd = &cobra.Command{
	Use:   "cd [--last имя] [--quiet]",
	Short: "Для оболочки: имя проекта в stdout, строка о нём в stderr при входе",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return nil
		}
		p, ok := cfg.ProjectHere(cwd)
		if !ok {
			return nil
		}
		fmt.Println(p.Name)
		if hookQuiet || p.Name == hookLast {
			return nil
		}
		ui.UseStderr()
		for _, l := range motdProject() {
			ui.Println(l)
		}
		return nil
	},
}

func init() {
	hookCdCmd.Flags().StringVar(&hookLast, "last", "", "проект, в котором оболочка уже была")
	hookCdCmd.Flags().BoolVar(&hookQuiet, "quiet", false, "только имя, без строки")
	hookCmd.AddCommand(hookCdCmd)
	root.AddCommand(hookCmd)
}
