package cmd

import (
	"fmt"
	"runtime/debug"
	"strings"

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

// shortVersion: для заставки. Версия go install вида v0.0.0-20251022162241-8c6e20c7cc4a+dirty
// длинная и ничего не говорит человеку - показываем коммит.
func shortVersion() string {
	v := resolveVersion()
	if i := strings.LastIndex(v, "-"); i > 0 && strings.HasPrefix(v, "v0.0.0-") {
		c := strings.TrimSuffix(v[i+1:], "+dirty")
		if len(c) > 7 {
			c = c[:7]
		}
		return c
	}
	return v
}
