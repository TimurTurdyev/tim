// Package gitx: вопросы к git о клонах проектов - состояние, последний коммит, что под git, журнал.
package gitx

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/runner"
)

// State: ветка и расхождения из git status -sb --porcelain.
type State struct {
	Branch  string
	Changes int
	Ahead   int
	Behind  int
	NoUp    bool
}

// ParseStatus разбирает git status -sb --porcelain.
func ParseStatus(out string) State {
	var g State
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "## ") {
		return g
	}
	head := strings.TrimPrefix(lines[0], "## ")
	g.Changes = len(lines) - 1
	if strings.HasPrefix(head, "No commits yet on ") {
		g.Branch, g.NoUp = strings.TrimPrefix(head, "No commits yet on "), true
		return g
	}
	branch, rest, hasUp := strings.Cut(head, "...")
	if !hasUp {
		g.Branch, g.NoUp = strings.Fields(head)[0], true
		return g
	}
	g.Branch = branch
	if i := strings.Index(rest, "["); i >= 0 {
		for _, part := range strings.Split(strings.Trim(rest[i:], "[]"), ", ") {
			k, v, _ := strings.Cut(part, " ")
			n, _ := strconv.Atoi(v)
			switch k {
			case "ahead":
				g.Ahead = n
			case "behind":
				g.Behind = n
			}
		}
	}
	return g
}

// IsRepo: в папке свой клон.
func IsRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// Status: ветка, изменения и расхождение с удалённой веткой; ok=false - не клон или git не ответил.
func Status(dir string) (State, bool) {
	out, err := runner.Output("git", "-C", dir, "status", "-sb", "--porcelain")
	if err != nil {
		return State{}, false
	}
	return ParseStatus(out), true
}

// LastCommit: время последнего коммита; нулевое - коммитов нет или это не клон.
func LastCommit(dir string) time.Time {
	out, err := runner.Output("git", "-C", dir, "log", "-1", "--format=%ct")
	if err != nil {
		return time.Time{}
	}
	sec, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}

// Tracked: файл или что-то в папке лежит под git.
func Tracked(path string) bool {
	out, err := runner.Output("git", "-C", filepath.Dir(path), "ls-files", "--", filepath.Base(path))
	return err == nil && strings.TrimSpace(out) != ""
}

// Ignored: файл попадает под .gitignore.
func Ignored(path string) bool {
	_, err := runner.Output("git", "-C", filepath.Dir(path), "check-ignore", "-q", filepath.Base(path))
	return err == nil
}

// Email: user.email этого клона - рабочие и личные проекты часто коммитятся с разных адресов.
func Email(dir string) string {
	out, _ := runner.Output("git", "-C", dir, "config", "user.email")
	return strings.TrimSpace(out)
}

// Log: git log по всем веткам без слияний с момента since, в формате format; author пусто - все авторы.
func Log(dir string, since time.Time, author, format string) (string, error) {
	args := []string{"-C", dir, "log", "--all", "--no-merges", "--since=" + since.Format(time.RFC3339), format}
	if author != "" {
		args = append(args, "--author="+author)
	}
	return runner.Output("git", args...)
}
