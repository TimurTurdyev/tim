package disk

import (
	"os"
	"path/filepath"

	"github.com/TimurTurdyev/tim/internal/paths"
)

// repoOf: корень git-репозитория над path - по нему видно, чей это node_modules или vendor и когда
// его трогали. Подъём - до домашней папки.
func repoOf(path string) (string, bool) {
	home := paths.Home()
	for d := path; d != "/" && d != home && d != "."; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return d, true
		}
	}
	return "", false
}

// repoName: имя репозитория над path; вне репозитория - пусто.
func repoName(path string) string {
	if d, ok := repoOf(path); ok {
		return filepath.Base(d)
	}
	return ""
}
