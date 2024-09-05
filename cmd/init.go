package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Шим нужен командам, которые меняют состояние самой оболочки: cd (go) и PATH (php use).
const shim = `export TIM_SHIM=1
tim() {
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

// shellQuote: значение в одинарных кавычках; кавычка внутри закрывает строку, экранируется и открывает её снова.
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

// rcFile: файл настроек для оболочки и имя самой оболочки.
func rcFile(shell string) (path, name string) {
	home, _ := os.UserHomeDir()
	if shell == "" {
		shell = filepath.Base(os.Getenv("SHELL"))
	}
	switch shell {
	case "bash":
		for _, f := range []string{".bashrc", ".bash_profile"} {
			if _, err := os.Stat(filepath.Join(home, f)); err == nil {
				return filepath.Join(home, f), "bash"
			}
		}
		return filepath.Join(home, ".bashrc"), "bash"
	default:
		return filepath.Join(home, ".zshrc"), "zsh"
	}
}

// sourced: строка подключения уже есть в этом файле или в том, что он подключает.
func sourced(path string, depth int) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	text := string(data)
	if strings.Contains(text, "tim init") {
		return true
	}
	if depth == 0 {
		return false
	}
	home, _ := os.UserHomeDir()
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		after, ok := strings.CutPrefix(line, "source ")
		if !ok {
			after, ok = strings.CutPrefix(line, ". ")
		}
		if !ok || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Trim(strings.TrimSpace(after), `"'`)
		f = strings.Replace(f, "$HOME", home, 1)
		f = strings.Replace(f, "~", home, 1)
		if !strings.Contains(f, "$") && sourced(f, depth-1) {
			return true
		}
	}
	return false
}

var initSetupCmd = &cobra.Command{
	Use:   "setup [zsh|bash]",
	Short: "Подключить tim к оболочке: дописать строку в её файл настроек",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path, shell := rcFile(first(args))
		line := `eval "$(tim init ` + shell + `)"`
		if sourced(path, 2) {
			ui.Println(ui.Label("уже подключено ") + ui.Dim(path))
		} else {
			if runner.DryRun {
				ui.Println(ui.Dim("dry-run: дописал бы в " + path + ": " + line))
			} else {
				f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
				if err != nil {
					return err
				}
				if _, err := f.WriteString("\n# tim\n" + line + "\n"); err != nil {
					_ = f.Close()
					return err
				}
				if err := f.Close(); err != nil {
					return err
				}
				ui.Println(ui.Label("подключено ") + path)
			}
		}
		if _, err := os.Stat(config.Path()); err != nil {
			ui.Println(ui.Warn("конфига нет") + ui.Dim("  собрать реестр проектов: tim config init"))
		} else {
			ui.Println(ui.Dim("конфиг на месте: " + config.Path()))
		}
		if os.Getenv("TIM_SHIM") == "" {
			ui.Println(ui.Dim("открой новый терминал, чтобы заработали tim go и tim php use"))
		}
		return nil
	},
}

var initCmd = &cobra.Command{
	Use:       "init [zsh|bash]",
	Short:     "Подключить tim к оболочке; с zsh или bash печатает код для eval",
	Long:      "Добавляет функцию tim (cd для tim go, PATH для tim php use), completion, подсказку о проекте при входе\nв его папку, алиасы и сводку из секции [shell] конфига.",
	Args:      cobra.MaximumNArgs(1),
	ValidArgs: []string{"zsh", "bash"},
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return initSetupCmd.RunE(cmd, nil)
		}
		s, err := shellScript(args[0])
		if err != nil {
			return err
		}
		fmt.Print(s)
		return nil
	},
}

func init() {
	initCmd.AddCommand(initSetupCmd)
	root.AddCommand(initCmd)
}
