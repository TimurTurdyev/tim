// Package newproj: раскладка и шаги создания нового проекта.
package newproj

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/TimurTurdyev/tim/internal/shellq"
)

// Layout: папка клона и папка над ней (обёртка или группа).
func Layout(root, in, name, wrapper string) (dir, parent string) {
	base := filepath.Join(root, in, name)
	if wrapper != "" {
		return filepath.Join(base, wrapper), base
	}
	return base, filepath.Dir(base)
}

// Expand подставляет {dir}, {name}, {parent} в шаг, экранируя значения для shell.
func Expand(step, dir, name, parent string) string {
	r := strings.NewReplacer("{dir}", shellq.Quote(dir), "{name}", shellq.Quote(name), "{parent}", shellq.Quote(parent))
	return r.Replace(step)
}

// Builtin: встроенные шаги по стекам; hasUV выбирает инициализацию Python.
func Builtin(stack string, hasUV bool) ([]string, bool) {
	switch stack {
	case "laravel":
		return []string{"composer create-project laravel/laravel {dir}"}, true
	case "php":
		return []string{"mkdir -p {dir}", "cd {dir} && composer init --no-interaction --name app/{name}"}, true
	case "go":
		return []string{"mkdir -p {dir}", "cd {dir} && go mod init {name}", `cd {dir} && printf 'package main\n\nfunc main() {}\n' > main.go`}, true
	case "node":
		return []string{"mkdir -p {dir}", "cd {dir} && npm init -y"}, true
	case "vite":
		return []string{"npm create vite@latest {dir} -- --template vanilla-ts"}, true
	case "python":
		if hasUV {
			return []string{"uv init {dir}"}, true
		}
		return []string{"mkdir -p {dir}", "cd {dir} && python3 -m venv .venv", `cd {dir} && printf 'def main():\n    pass\n\n\nif __name__ == "__main__":\n    main()\n' > main.py`}, true
	case "empty":
		return []string{"mkdir -p {dir}"}, true
	}
	return nil, false
}

func Stacks() []string {
	s := []string{"laravel", "php", "go", "node", "vite", "python", "empty"}
	sort.Strings(s)
	return s
}

// NextPort: после самого большого порта реестра (не меньше base), первый не занятый в системе.
func NextPort(used []int, base int, busy func(string) bool) int {
	p := base
	for _, u := range used {
		if u >= p {
			p = u + 1
		}
	}
	for busy(strconv.Itoa(p)) {
		p++
	}
	return p
}
