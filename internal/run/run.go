// Package run: как запустить проект локально, по файлам в его папке.
package run

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/TimurTurdyev/tim/internal/registry"
)

// Facts: всё, что нужно знать о папке, чтобы выбрать команду запуска.
type Facts struct {
	Stack       string
	ComposerDev bool
	NodeDev     bool
	MakeDev     bool
	PkgManager  string
	Public      bool
	UV          bool
	Python      string
	PyEntry     string
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// Detect собирает факты о папке проекта.
func Detect(dir string) Facts {
	f := Facts{Stack: registry.Stack(dir), PkgManager: "npm"}
	f.ComposerDev = hasScript(filepath.Join(dir, "composer.json"), "dev")
	f.NodeDev = hasScript(filepath.Join(dir, "package.json"), "dev")
	f.MakeDev = hasMakeTarget(filepath.Join(dir, "Makefile"), "dev")
	for lock, pm := range map[string]string{"pnpm-lock.yaml": "pnpm", "yarn.lock": "yarn", "bun.lockb": "bun", "bun.lock": "bun"} {
		if exists(filepath.Join(dir, lock)) {
			f.PkgManager = pm
		}
	}
	f.Public = exists(filepath.Join(dir, "public"))
	f.UV = exists(filepath.Join(dir, "uv.lock"))
	for _, v := range []string{".venv", "venv"} {
		if py := filepath.Join(dir, v, "bin", "python"); exists(py) {
			f.Python = py
			break
		}
	}
	for _, e := range []string{"manage.py", "main.py", "app.py", "server.py"} {
		if exists(filepath.Join(dir, e)) {
			f.PyEntry = e
			break
		}
	}
	return f
}

func hasScript(path, name string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var v struct {
		Scripts map[string]json.RawMessage `json:"scripts"`
	}
	if json.Unmarshal(data, &v) != nil {
		return false
	}
	_, ok := v.Scripts[name]
	return ok
}

func hasMakeTarget(path, target string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), target+":") {
			return true
		}
	}
	return false
}

// Command: что запустить; Env добавляется к окружению.
type Command struct {
	Name string
	Args []string
	Env  []string
}

func (c Command) String() string {
	return strings.TrimSpace(strings.Join(append(c.Env, append([]string{c.Name}, c.Args...)...), " "))
}

// Serve выбирает команду dev-сервера для стека.
func Serve(f Facts, port string) (Command, error) {
	makeDev := Command{Name: "make", Args: []string{"dev"}}
	switch f.Stack {
	case "laravel":
		if f.ComposerDev {
			return Command{Name: "composer", Args: []string{"dev"}, Env: []string{"SERVER_PORT=" + port}}, nil
		}
		return Command{Name: "php", Args: []string{"artisan", "serve", "--port=" + port}}, nil
	case "php":
		root := "."
		if f.Public {
			root = "public"
		}
		return Command{Name: "php", Args: []string{"-S", "127.0.0.1:" + port, "-t", root}}, nil
	case "node", "vitepress":
		if f.MakeDev {
			return makeDev, nil
		}
		if f.NodeDev {
			return Command{Name: f.PkgManager, Args: []string{"run", "dev", "--", "--port", port}}, nil
		}
		return Command{}, errors.New("в package.json нет скрипта dev")
	case "go":
		if f.MakeDev {
			return makeDev, nil
		}
		return Command{Name: "go", Args: []string{"run", "."}, Env: []string{"PORT=" + port}}, nil
	case "python":
		if f.MakeDev {
			return makeDev, nil
		}
		if f.PyEntry == "" {
			return Command{}, errors.New("не нашёл manage.py, main.py, app.py или server.py")
		}
		args := []string{f.PyEntry}
		if f.PyEntry == "manage.py" {
			args = append(args, "runserver", port)
		}
		switch {
		case f.UV:
			return Command{Name: "uv", Args: append([]string{"run", "python"}, args...)}, nil
		case f.Python != "":
			return Command{Name: f.Python, Args: args}, nil
		}
		return Command{Name: "python3", Args: args}, nil
	}
	if f.MakeDev {
		return makeDev, nil
	}
	return Command{}, errors.New("стек " + f.Stack + " не знаю, а цели dev в Makefile нет")
}
