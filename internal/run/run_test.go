package run

import (
	"os"
	"path/filepath"
	"testing"
)

func TestServe(t *testing.T) {
	cases := []struct {
		name string
		f    Facts
		want string
	}{
		{"laravel composer dev", Facts{Stack: "laravel", ComposerDev: true}, "SERVER_PORT=8001 composer dev"},
		{"laravel artisan", Facts{Stack: "laravel"}, "php artisan serve --port=8001"},
		{"plain php public", Facts{Stack: "php", Public: true}, "php -S 127.0.0.1:8001 -t public"},
		{"plain php root", Facts{Stack: "php"}, "php -S 127.0.0.1:8001 -t ."},
		{"node make", Facts{Stack: "node", MakeDev: true, NodeDev: true}, "make dev"},
		{"node pnpm", Facts{Stack: "vitepress", NodeDev: true, PkgManager: "pnpm"}, "pnpm run dev -- --port 8001"},
		{"go run", Facts{Stack: "go"}, "PORT=8001 go run ."},
		{"django venv", Facts{Stack: "python", PyEntry: "manage.py", Python: "/p/.venv/bin/python"}, "/p/.venv/bin/python manage.py runserver 8001"},
		{"python uv", Facts{Stack: "python", PyEntry: "main.py", UV: true}, "uv run python main.py"},
		{"python plain", Facts{Stack: "python", PyEntry: "app.py"}, "python3 app.py"},
		{"other make", Facts{Stack: "other", MakeDev: true}, "make dev"},
		{"compose", Facts{Stack: "other", Compose: "compose.yml"}, "docker compose up"},
		{"compose in docker/", Facts{Stack: "other", Compose: "docker/docker-compose.yml"}, "docker compose -f docker/docker-compose.yml up"},
		{"make beats compose", Facts{Stack: "other", Compose: "compose.yml", MakeDev: true}, "make dev"},
		{"custom wins", Facts{Stack: "laravel", ComposerDev: true, Custom: "npm run serve"}, "PORT=8001 sh -c npm run serve"},
	}
	for _, c := range cases {
		got, err := Serve(c.f, "8001")
		if err != nil || got.String() != c.want {
			t.Errorf("%s: %q %v", c.name, got.String(), err)
		}
	}
	for _, f := range []Facts{{Stack: "node"}, {Stack: "python"}, {Stack: "other"}} {
		if _, err := Serve(f, "8001"); err == nil {
			t.Errorf("%+v should fail", f)
		}
	}
}

func TestDetect(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("package.json", `{"scripts":{"dev":"vite","build":"vite build"}}`)
	write("pnpm-lock.yaml", "")
	write("Makefile", "build:\n\tgo build\ndev-tools:\n\techo\n")
	f := Detect(dir)
	if f.Stack != "node" || !f.NodeDev || f.MakeDev || f.PkgManager != "pnpm" {
		t.Errorf("facts %+v", f)
	}
	write("Makefile", "dev:\n\tnpm run dev\n")
	if !Detect(dir).MakeDev {
		t.Error("make dev not found")
	}
}

func TestMatchNames(t *testing.T) {
	names := []string{"lpack-site", "lpack-kb", "mercurio-lpack", "lpack", "shop"}
	if got := MatchNames(names, "LPACK"); len(got) != 1 || got[0] != 3 {
		t.Errorf("exact: %v", got)
	}
	if got := MatchNames(names[:3], "lpack"); len(got) != 2 || got[0] != 0 || got[1] != 1 {
		t.Errorf("prefix over substring: %v", got)
	}
	if got := MatchNames(names, "merc"); len(got) != 1 || got[0] != 2 {
		t.Errorf("single prefix: %v", got)
	}
	if got := MatchNames(names, "kb"); len(got) != 1 || got[0] != 1 {
		t.Errorf("substring: %v", got)
	}
	if got := MatchNames(names, "zzz"); len(got) != 0 {
		t.Errorf("none: %v", got)
	}
}

func TestAllocPorts(t *testing.T) {
	busy := map[string]bool{"8003": true, "8001": true}
	got := AllocPorts([]string{"8000", "8000", "8003", "", "8002"}, func(p string) bool { return busy[p] })
	want := []string{"8000", "8002", "8004", "8005", "8006"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}
