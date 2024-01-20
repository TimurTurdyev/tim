package cmd

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// version задаётся при сборке: -ldflags "-X github.com/TimurTurdyev/tim/cmd.version=v0.1.0".
var version = ""

func resolveVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Версия tim",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println(resolveVersion())
		return nil
	},
}

func init() {
	root.Version = resolveVersion()
	root.AddCommand(versionCmd)
}
