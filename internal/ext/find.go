package ext

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/runner"
)

const describeWait = time.Second

var nameRe = regexp.MustCompile(`^tim-([a-z0-9][a-z0-9-]*)$`)

// Command: своя команда - имя после tim- и путь к файлу.
type Command struct {
	Name, Path string
}

// Dir: свои команды рядом с конфигом - их видно раньше, чем одноимённые из PATH.
func Dir() string { return filepath.Join(filepath.Dir(config.Path()), "commands") }

func dirs() []string { return append([]string{Dir()}, filepath.SplitList(os.Getenv("PATH"))...) }

// Find: все свои команды по имени; при одинаковом имени выигрывает первая по Dir и PATH.
func Find() []Command {
	seen := map[string]bool{}
	var out []Command
	for _, dir := range dirs() {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			m := nameRe.FindStringSubmatch(e.Name())
			if m == nil || seen[m[1]] {
				continue
			}
			if path := filepath.Join(dir, e.Name()); executable(path) {
				seen[m[1]] = true
				out = append(out, Command{Name: m[1], Path: path})
			}
		}
	}
	slices.SortFunc(out, func(a, b Command) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// Lookup: одна своя команда - без обхода всех каталогов PATH.
func Lookup(name string) (Command, bool) {
	if !nameRe.MatchString("tim-" + name) {
		return Command{}, false
	}
	for _, dir := range dirs() {
		if path := filepath.Join(dir, "tim-"+name); executable(path) {
			return Command{Name: name, Path: path}, true
		}
	}
	return Command{}, false
}

// Describe: строка для справки - первая строка ответа на --tim-describe; молчит или падает - пусто.
func Describe(c Command) string {
	out, err := runner.OutputWithin(describeWait, c.Path, "--tim-describe")
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(strings.TrimSpace(out), "\n")
	return line
}

// Run: своя команда с аргументами как есть. Окружение говорит ей, как её позвали: TIM_JSON, TIM_DRY_RUN,
// TIM_CONFIG и TIM_BIN - чем звать tim render.
func Run(c Command, args []string) error {
	env := []string{"TIM_CONFIG=" + config.Path()}
	if self, err := os.Executable(); err == nil {
		env = append(env, "TIM_BIN="+self)
	}
	if runner.DryRun || slices.Contains(args, "--dry-run") {
		env = append(env, "TIM_DRY_RUN=1")
	}
	if slices.Contains(args, "--json") {
		env = append(env, "TIM_JSON=1")
	}
	err := runner.Handover(c.Path, args, env)
	var coded interface{ ExitCode() int }
	if errors.As(err, &coded) {
		return app.ExitCode(coded.ExitCode())
	}
	return err
}

func executable(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
}
