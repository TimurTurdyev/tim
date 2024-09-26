package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// Шим нужен командам, которые меняют состояние самой оболочки: cd (go) и PATH (php use).
const shim = `tim() {
  case "$1" in
    go)
      local d
      d=$(command tim go --print "${@:2}") || return $?
      [[ -n "$d" ]] && cd "$d" ;;
    php)
      if [[ "$2" == use && -n "$3" ]]; then
        local e
        e=$(command tim php use --print "$3") || return $?
        eval "$e"
      else
        command tim "$@"
      fi ;;
    *) command tim "$@" ;;
  esac
}
`

const zshHook = `_tim_cd() { _TIM_PROJECT=$(command tim hook cd --last "$_TIM_PROJECT") }
autoload -Uz add-zsh-hook
add-zsh-hook chpwd _tim_cd
`

const bashHook = `_tim_prompt() {
  if [[ "$PWD" != "$_TIM_PWD" ]]; then
    _TIM_PWD=$PWD
    _TIM_PROJECT=$(command tim hook cd --last "$_TIM_PROJECT")
  fi
}
case ";$PROMPT_COMMAND;" in *";_tim_prompt;"*) ;; *) PROMPT_COMMAND="_tim_prompt${PROMPT_COMMAND:+;$PROMPT_COMMAND}" ;; esac
`

// shellQuote: значение в одинарных кавычках, сама кавычка пишется как '\''.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func validAlias(name string) bool {
	if name == "" || name == "tim" {
		return false
	}
	for _, r := range name {
		ok := r == '_' || r == '-' || r == '.' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
		if !ok {
			return false
		}
	}
	return true
}

func shellScript(shell string) (string, error) {
	var b strings.Builder
	b.WriteString(shim)
	// Не return: eval идёт внутри .zshrc, return оборвал бы его остаток.
	switch shell {
	case "zsh":
		b.WriteString("if (( $+functions[compdef] )); then source <(command tim completion zsh); compdef _tim tim; fi\n")
		b.WriteString("if [[ -o interactive ]]; then\n")
		if cfg.Shell.CdInfoOn() {
			b.WriteString(zshHook)
		}
	case "bash":
		b.WriteString("source <(command tim completion bash)\n")
		b.WriteString("if [[ $- == *i* ]]; then\n")
		if cfg.Shell.CdInfoOn() {
			b.WriteString(bashHook)
		}
	default:
		return "", fmt.Errorf("поддерживаются zsh и bash")
	}
	names := make([]string, 0, len(cfg.Shell.Aliases))
	for n := range cfg.Shell.Aliases {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if validAlias(n) {
			fmt.Fprintf(&b, "alias %s=%s\n", n, shellQuote(cfg.Shell.Aliases[n]))
		}
	}
	switch {
	case cfg.Shell.Motd:
		b.WriteString("command tim motd\n")
		if cfg.Shell.CdInfoOn() {
			b.WriteString("_TIM_PROJECT=$(command tim hook cd --quiet)\n")
		}
	case cfg.Shell.CdInfoOn() && shell == "zsh":
		b.WriteString("_tim_cd\n")
	}
	b.WriteString("fi\n")
	return b.String(), nil
}

var initCmd = &cobra.Command{
	Use:       "init zsh|bash",
	Short:     "Код для .zshrc или .bashrc: eval \"$(tim init zsh)\"",
	Long:      "Добавляет функцию tim (cd для tim go, PATH для tim php use), completion, подсказку о проекте при входе\nв его папку, алиасы и сводку из секции [shell] конфига.",
	Args:      cobra.ExactArgs(1),
	ValidArgs: []string{"zsh", "bash"},
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := shellScript(args[0])
		if err != nil {
			return err
		}
		fmt.Print(s)
		return nil
	},
}

func init() { root.AddCommand(initCmd) }
