package paths

import (
	"path/filepath"
	"strings"
)

// Show: путь с ~ вместо домашней папки.
func Show(p string) string {
	home := Home()
	if r, err := filepath.Rel(home, p); err == nil && !strings.HasPrefix(r, "..") {
		if r == "." {
			return "~"
		}
		return "~/" + r
	}
	return p
}
