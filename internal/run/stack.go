package run

import (
	"path/filepath"
)

// Stack: laravel, vitepress, go, python, php, node, make или other.
func Stack(dir string) string {
	switch {
	case exists(filepath.Join(dir, "artisan")):
		return "laravel"
	case exists(filepath.Join(dir, ".vitepress")):
		return "vitepress"
	case exists(filepath.Join(dir, "go.mod")):
		return "go"
	case exists(filepath.Join(dir, "pyproject.toml")), exists(filepath.Join(dir, "requirements.txt")),
		exists(filepath.Join(dir, ".venv")), exists(filepath.Join(dir, "venv")):
		return "python"
	case exists(filepath.Join(dir, "composer.json")):
		return "php"
	case exists(filepath.Join(dir, "package.json")):
		return "node"
	case exists(filepath.Join(dir, "Makefile")):
		return "make"
	}
	return "other"
}
