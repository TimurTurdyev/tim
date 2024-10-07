package setup

import (
	"strings"
	"testing"
)

func state(stack string, files ...string) State {
	s := State{Stack: stack, Has: map[string]bool{}, Env: map[string]string{}}
	for _, f := range files {
		s.Has[f] = true
	}
	return s
}

func plan(s State) string {
	var parts []string
	for _, st := range Plan(s) {
		parts = append(parts, st.String())
	}
	return strings.Join(parts, " | ")
}

func TestFreshLaravel(t *testing.T) {
	s := state("laravel", "artisan", ".env.example", "composer.json", "package.json", "storage")
	got := plan(s)
	want := "cp .env.example .env | composer install | npm install | php artisan key:generate | " +
		"touch database/database.sqlite | php artisan migrate | php artisan storage:link"
	if got != want {
		t.Fatalf("план\n got: %s\nwant: %s", got, want)
	}
}

func TestLaravelAlreadySet(t *testing.T) {
	s := state("laravel", "artisan", ".env", ".env.example", "composer.json", "vendor",
		"package.json", "node_modules", "storage", "public/storage")
	s.Env["APP_KEY"] = "base64:xxx"
	s.Env["DB_CONNECTION"] = "pgsql"
	if got := plan(s); got != "" {
		t.Fatalf("готовому проекту делать нечего, получили: %s", got)
	}
}

func TestMigrateAsks(t *testing.T) {
	steps := Plan(state("laravel", "artisan", ".env.example", "composer.json"))
	for _, st := range steps {
		if st.Cmd != nil && st.Cmd[len(st.Cmd)-1] == "migrate" && !st.Ask {
			t.Fatal("migrate должен спрашивать подтверждение")
		}
	}
}

func TestNonLaravel(t *testing.T) {
	cases := map[string]struct {
		s    State
		want string
	}{
		"go":     {state("go", "go.mod"), "go mod download"},
		"node":   {state("node", "package.json"), "npm install"},
		"pnpm":   {func() State { s := state("node", "package.json"); s.PkgManager = "pnpm"; return s }(), "pnpm install"},
		"uv":     {state("python", "pyproject.toml", "uv.lock"), "uv sync"},
		"pip":    {state("python", "requirements.txt"), "python3 -m venv .venv | .venv/bin/pip install -r requirements.txt"},
		"готово": {state("python", "requirements.txt", ".venv"), ""},
		"пусто":  {state("other"), ""},
	}
	for name, c := range cases {
		if got := plan(c.s); got != c.want {
			t.Errorf("%s: получили %q, ожидали %q", name, got, c.want)
		}
	}
}

func TestSqliteFromEnv(t *testing.T) {
	s := state("laravel", "artisan", ".env", "vendor")
	s.Env["APP_KEY"] = "x"
	s.Env["DB_CONNECTION"] = "sqlite"
	s.Env["DB_DATABASE"] = "database/db.sqlite"
	if got := plan(s); got != "touch database/db.sqlite | php artisan migrate" { // база создана - миграции нужны
		t.Fatalf("получили %q", got)
	}
	s.Has["database/db.sqlite"] = true
	if got := plan(s); got != "" {
		t.Fatalf("существующий файл базы не должен создаваться заново: %q", got)
	}
}
