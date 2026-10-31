// Package gitx: два вопроса к git, которые нужны Диску: когда в репозитории последний раз коммитили
// и лежит ли папка под git - такую в Корзину не убирают.
package gitx

import (
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/runner"
)

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
