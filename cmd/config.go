package cmd

import (
	"os"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Где лежит конфиг и что в нём",
	RunE: func(cmd *cobra.Command, args []string) error {
		ui.Println(ui.Dim(config.Path()))
		return toml.NewEncoder(os.Stdout).Encode(cfg)
	},
}

var configMigrateCmd = &cobra.Command{
	Use:   "migrate <папка с tools.projects, tools.hosts, tools.conf>",
	Short: "Собрать конфиг из файлов zsh-версии tools",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := config.Migrate(args[0])
		if err != nil {
			return err
		}
		if _, err := os.Stat(config.Path()); err == nil {
			if !confirm("конфиг " + config.Path() + " уже есть, перезаписать?") {
				return nil
			}
		}
		if err := config.Save(c); err != nil {
			return err
		}
		ui.Printf("%s %s: проектов %d, хостов %d, туннель %s\n", ui.Label("записан"), config.Path(), len(c.Projects), len(c.Hosts), c.Tunnel.Host)
		return nil
	},
}

func init() {
	configCmd.AddCommand(configMigrateCmd)
	root.AddCommand(configCmd)
}
