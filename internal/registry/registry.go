// Package registry: стек проекта по файлам и выбор проекта по аргументу или папке.
package registry

import (
	"os"
	"path/filepath"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/ui"
)

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

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

// Pick: проект по имени, а без имени по текущей папке.
func Pick(cfg *config.Config, name string) (*config.Project, error) {
	if name != "" {
		p, ok := cfg.Project(name)
		if !ok {
			return nil, ui.Errorf("нет проекта %s в конфиге: tim proj list", name)
		}
		return p, nil
	}
	if len(cfg.Projects) == 0 {
		return nil, ui.Errorf("реестр пуст: tim config init или tim proj scan")
	}
	cwd, _ := os.Getwd()
	if p, ok := cfg.ProjectHere(cwd); ok {
		return p, nil
	}
	return nil, ui.Errorf("укажи проект или зайди в его папку: tim proj list")
}
