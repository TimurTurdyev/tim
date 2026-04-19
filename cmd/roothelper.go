package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/privileged"
)

// Скрытая команда, под которой sudo запускает корневого помощника; сам он в internal/privileged.
var rootHelperCmd = &cobra.Command{
	Use:    privileged.HelperArg,
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE:   func(cmd *cobra.Command, args []string) error { return privileged.Serve(os.Stdin, os.Stdout) },
}

func init() {
	root.AddCommand(rootHelperCmd)
}
