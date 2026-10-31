// Package stack: чем собрана папка проекта - Laravel, Vite, Go, Python - по файлам в её корне.
package stack

import (
	"os"
	"path/filepath"
)

// names: как стек называется на экране; "other" и "make" - без имени.
var names = map[string]string{"laravel": "Laravel", "node": "Node.js", "php": "PHP", "go": "Go",
	"python": "Python", "vitepress": "VitePress"}

// Name: стек папки словами для списков; неизвестный - пусто.
func Name(dir string) string { return names[Of(dir)] }

// Of: laravel, vitepress, go, python, php, node, make или other.
func Of(dir string) string {
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

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
