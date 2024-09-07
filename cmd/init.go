package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Шим нужен командам, которые меняют состояние самой оболочки: cd и переменные.
const zshShim = `tim() {
  case "$1" in
    go)
      local d
      d=$(command tim go --print "${@:2}") || return $?
      [[ -n "$d" ]] && cd "$d" ;;
    *) command tim "$@" ;;
  esac
}
`

var initCmd = &cobra.Command{
	Use:   "init zsh",
	Short: "Код для .zshrc: eval \"$(tim init zsh)\"",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if args[0] != "zsh" {
			return fmt.Errorf("пока только zsh")
		}
		fmt.Print(zshShim)
		fmt.Println(`if (( $+functions[compdef] )); then source <(command tim completion zsh); compdef _tim tim; fi`)
		return nil
	},
}

func init() { root.AddCommand(initCmd) }
