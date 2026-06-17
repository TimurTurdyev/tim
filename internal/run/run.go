// Package run: как запустить проект локально, по файлам в его папке.
package run

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/TimurTurdyev/tim/internal/netinfo"
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
	Compose     string
	Custom      string
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// Detect собирает факты о папке проекта.
func Detect(dir string) Facts {
	f := Facts{Stack: Stack(dir), PkgManager: "npm"}
	f.ComposerDev = hasScript(filepath.Join(dir, "composer.json"), "dev")
	f.NodeDev = hasScript(filepath.Join(dir, "package.json"), "dev")
	f.MakeDev = hasMakeTarget(filepath.Join(dir, "Makefile"), "dev")
	for _, l := range [...]struct{ lock, pm string }{{"pnpm-lock.yaml", "pnpm"}, {"bun.lock", "bun"}, {"bun.lockb", "bun"}, {"yarn.lock", "yarn"}} {
		if exists(filepath.Join(dir, l.lock)) {
			f.PkgManager = l.pm
			break
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
compose:
	for _, sub := range []string{".", "docker"} {
		for _, c := range []string{"compose.yml", "compose.yaml", "docker-compose.yml", "docker-compose.yaml"} {
			if exists(filepath.Join(dir, sub, c)) {
				f.Compose = filepath.Join(sub, c)
				break compose
			}
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
	if f.Custom != "" {
		return Command{Name: "sh", Args: []string{"-c", f.Custom}, Env: []string{"PORT=" + port}}, nil
	}
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
	if f.Compose != "" {
		if filepath.Dir(f.Compose) == "." {
			return Command{Name: "docker", Args: []string{"compose", "up"}}, nil
		}
		return Command{Name: "docker", Args: []string{"compose", "-f", f.Compose, "up"}}, nil
	}
	return Command{}, errors.New("стек " + f.Stack + " не знаю, цели dev в Makefile и compose-файла нет; задай serve в конфиге проекта")
}

// MatchNames: точное имя, иначе начало имени, иначе вхождение; индексы совпадений.
func MatchNames(names []string, q string) []int {
	q = strings.ToLower(q)
	var prefix, sub []int
	for i, n := range names {
		l := strings.ToLower(n)
		switch {
		case l == q:
			return []int{i}
		case strings.HasPrefix(l, q):
			prefix = append(prefix, i)
		case strings.Contains(l, q):
			sub = append(sub, i)
		}
	}
	if len(prefix) > 0 {
		return prefix
	}
	return sub
}

// AllocPorts раздаёт порты по порядку: занятый или уже выданный заменяется следующим свободным.
func AllocPorts(wanted []string, busy func(string) bool) []string {
	used := map[int]bool{}
	taken := func(p int) bool { return used[p] || busy(strconv.Itoa(p)) }
	out := make([]string, len(wanted))
	for i, w := range wanted {
		p, err := strconv.Atoi(w)
		if err != nil || p < 1 {
			p = netinfo.DefaultPort
		}
		if free, ok := netinfo.FirstFree(p, taken); ok {
			p = free
		}
		used[p] = true
		out[i] = strconv.Itoa(p)
	}
	return out
}
