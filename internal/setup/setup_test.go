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
