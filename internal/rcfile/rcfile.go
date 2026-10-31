// Package rcfile: файл настроек оболочки и код, который tim init подключает к zsh и bash.
package rcfile

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/shellq"
)

// Шим нужен экранам: перейти в папку может только сама оболочка, tim пишет её в TIM_CD_FILE.
const shim = `export TIM_SHIM=1
tim() {
  local f d r
  f=$(mktemp "${TMPDIR:-/tmp}/tim-cd.XXXXXX") || { command tim "$@"; return $?; }
  TIM_CD_FILE=$f command tim "$@"; r=$?
  d=$(cat "$f" 2>/dev/null); rm -f "$f"
  [[ -n "$d" ]] && cd "$d"
  return $r
}
`

// Script: код для eval "$(tim init zsh)" - шим, completion, алиасы и сводка.
func Script(shell string, sh config.Shell) (string, error) {
	var b strings.Builder
	b.WriteString(shim)
	// Не return: eval идёт внутри .zshrc, return оборвал бы его остаток.
	switch shell {
	case "zsh":
		b.WriteString("if (( $+functions[compdef] )); then source <(command tim completion zsh); compdef _tim tim; fi\n")
		b.WriteString("if [[ -o interactive ]]; then\n")
	case "bash":
		b.WriteString("source <(command tim completion bash)\n")
		b.WriteString("if [[ $- == *i* ]]; then\n")
	default:
		return "", fmt.Errorf("поддерживаются zsh и bash")
	}
	names := make([]string, 0, len(sh.Aliases))
	for n := range sh.Aliases {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if validAlias(n) {
			fmt.Fprintf(&b, "alias %s=%s\n", n, shellq.Quote(sh.Aliases[n]))
		}
	}
	if sh.Motd {
		// Один раз на окно: вложенные оболочки наследуют переменную и сводку не повторяют.
		b.WriteString("if [[ -z \"$TIM_MOTD_SHOWN\" ]]; then export TIM_MOTD_SHOWN=1; command tim status; fi\n")
	}
	b.WriteString("fi\n")
	return b.String(), nil
}

// Path: файл настроек оболочки и её имя; пустое shell - оболочка из $SHELL. tim подключается к zsh и
// bash; другая оболочка (sh, dash, ash, fish) - ошибка с подсказкой, а не строка в ~/.zshrc, который
// никто не читает. Без $SHELL (cron, голый ssh) - та, что есть в системе: сначала zsh, потом bash.
func Path(shell string) (path, name string, err error) {
	home := paths.Home()
	if shell == "" {
		shell = filepath.Base(os.Getenv("SHELL"))
	}
	if shell == "" || shell == "." {
		shell = "bash"
		if runner.Has("zsh") {
			shell = "zsh"
		}
	}
	switch shell {
	case "zsh":
		return filepath.Join(home, ".zshrc"), "zsh", nil
	case "bash":
		for _, f := range []string{".bashrc", ".bash_profile"} {
			if _, err := os.Stat(filepath.Join(home, f)); err == nil {
				return filepath.Join(home, f), "bash", nil
			}
		}
		return filepath.Join(home, ".bashrc"), "bash", nil
	}
	return "", shell, fmt.Errorf("оболочка %s: tim подключается к zsh и bash; сменить оболочку: chsh -s $(command -v bash)", shell)
}

// Sourced: строка подключения tim init уже есть в файле или в тех, что он подключает, не глубже depth.
func Sourced(path string, depth int) bool {
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
	for _, f := range includes(text, paths.Home()) {
		if Sourced(f, depth-1) {
			return true
		}
	}
	return false
}

// Append дописывает строку подключения в конец файла; в dry-run только печатает.
func Append(path, line string) error {
	if runner.DryRun {
		runner.Note("дописал бы в " + path + ": " + line)
		return nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString("\n# tim\n" + line + "\n"); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// includes: файлы из строк source и ".", с раскрытыми ~ и $HOME; пути с другими переменными не угадать.
func includes(text, home string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		after, ok := strings.CutPrefix(line, "source ")
		if !ok {
			after, ok = strings.CutPrefix(line, ". ")
		}
		if !ok {
			continue
		}
		f := strings.Trim(strings.TrimSpace(after), `"'`)
		f = strings.Replace(f, "$HOME", home, 1)
		f = strings.Replace(f, "~", home, 1)
		if !strings.Contains(f, "$") {
			out = append(out, f)
		}
	}
	return out
}

// validAlias: имя алиаса, которое безопасно вставить в код оболочки.
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
