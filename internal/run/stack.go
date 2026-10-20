package run

import (
	"path/filepath"
)

// Stack: laravel, vitepress, go, python, php, node, make или other.
// stackNames: как стек называется на экране; "other" и "make" - без имени.
var stackNames = map[string]string{"laravel": "Laravel", "node": "Node.js", "php": "PHP", "go": "Go",
	"python": "Python", "vitepress": "VitePress"}

// StackName: стек проекта словами для списков и плиток; неизвестный - пусто.
func StackName(dir string) string { return stackNames[Stack(dir)] }

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
