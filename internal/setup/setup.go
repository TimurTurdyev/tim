// Package setup: что нужно сделать в только что склонированном проекте, чтобы он заработал.
package setup

import "strings"

// Step: один шаг подготовки. Вместо Cmd может быть заполнен Copy или Touch - работа с файлами,
// которую делаем сами, чтобы не зависеть от внешних утилит и создать недостающие папки.
type Step struct {
	Why   string
	Cmd   []string
	Copy  [2]string
	Touch string
	Ask   bool // шаг меняет данные, спрашиваем отдельно
}

func (s Step) String() string {
	switch {
	case s.Touch != "":
		return "touch " + s.Touch
	case s.Cmd == nil:
		return "cp " + s.Copy[0] + " " + s.Copy[1]
	}
	return strings.Join(s.Cmd, " ")
}

// State: что уже есть в папке проекта. Отделено от файловой системы, чтобы план можно было проверить.
type State struct {
	Stack      string
	PkgManager string
	Has        map[string]bool
	Env        map[string]string
}

func (s State) has(path string) bool { return s.Has[path] }

// Plan: шаги в том порядке, в котором их надо выполнять. Уже сделанное не повторяется.
func Plan(s State) []Step {
	var steps []Step
	envFresh := false
	if !s.has(".env") {
		for _, src := range []string{".env.example", ".env.sample", ".env.dist"} {
			if s.has(src) {
				steps = append(steps, Step{Why: "конфигурация", Copy: [2]string{src, ".env"}})
				envFresh = true
				break
			}
		}
	}
	if s.has("composer.json") && !s.has("vendor") {
		steps = append(steps, Step{Why: "зависимости php", Cmd: []string{"composer", "install"}})
	}
	if s.has("package.json") && !s.has("node_modules") {
		pm := s.PkgManager
		if pm == "" {
			pm = "npm"
		}
		steps = append(steps, Step{Why: "зависимости js", Cmd: []string{pm, "install"}})
	}
	if s.has("go.mod") {
		steps = append(steps, Step{Why: "зависимости go", Cmd: []string{"go", "mod", "download"}})
	}
	switch {
	case s.has("uv.lock"), s.has("pyproject.toml") && s.has("uv.lock"):
		steps = append(steps, Step{Why: "зависимости python", Cmd: []string{"uv", "sync"}})
	case s.has("requirements.txt") && !s.has(".venv") && !s.has("venv"):
		steps = append(steps,
			Step{Why: "окружение python", Cmd: []string{"python3", "-m", "venv", ".venv"}},
			Step{Why: "зависимости python", Cmd: []string{".venv/bin/pip", "install", "-r", "requirements.txt"}})
	}
	if s.Stack != "laravel" {
		return steps
	}
	if envFresh || s.Env["APP_KEY"] == "" {
		steps = append(steps, Step{Why: "ключ приложения", Cmd: []string{"php", "artisan", "key:generate"}})
	}
	// Laravel с sqlite не стартует без самого файла базы, драйвер его не создаёт.
	if db := s.Env["DB_CONNECTION"]; db == "sqlite" || envFresh && db == "" {
		if path := sqlitePath(s); path != "" && !s.has(path) {
			steps = append(steps, Step{Why: "файл базы", Touch: path})
		}
	}
	// Миграции нужны только если проект и правда поднимали с нуля; у готового проекта команда молчит.
	if len(steps) > 0 {
		steps = append(steps, Step{Why: "схема базы", Cmd: []string{"php", "artisan", "migrate"}, Ask: true})
	}
	if s.has("storage") && !s.has("public/storage") {
		steps = append(steps, Step{Why: "ссылка на хранилище", Cmd: []string{"php", "artisan", "storage:link"}})
	}
	return steps
}

func sqlitePath(s State) string {
	if p := s.Env["DB_DATABASE"]; strings.HasSuffix(p, ".sqlite") && !strings.HasPrefix(p, "/") {
		return p
	}
	if s.Env["DB_DATABASE"] == "" || s.Env["DB_CONNECTION"] == "sqlite" {
		return "database/database.sqlite"
	}
	return ""
}
