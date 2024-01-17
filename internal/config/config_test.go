package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateAndHere(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("tools.projects", "# c\nshort\tprojects/b/app\n\na\tprojects/a/app\tssh-a\t/srv/a\tmake:deploy\t5180\tpg:a\na-hb\tprojects/a/app/handbook\n")
	write("tools.hosts", "# list\n203.0.113.10 site\n203.0.113.20\n")
	write("tools.conf", "TOOLS_TUNNEL_HOST=srv\nTOOLS_TUNNEL_PORT=1081\n")
	cfg, err := Migrate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Projects) != 3 {
		t.Fatalf("projects: want 3, got %d", len(cfg.Projects))
	}
	a, ok := cfg.Project("a")
	if !ok || a.SSH != "ssh-a" || a.Remote != "/srv/a" || a.Deploy != "make:deploy" || a.Port != 5180 || a.DB != "pg:a" {
		t.Errorf("project a parsed wrong: %+v", a)
	}
	if s, _ := cfg.Project("short"); s.SSH != "" || s.Port != 0 {
		t.Errorf("short line must have empty fields: %+v", s)
	}
	if cfg.Tunnel.Host != "srv" || cfg.Tunnel.Port != 1081 {
		t.Errorf("tunnel: %+v", cfg.Tunnel)
	}
	if len(cfg.Hosts) != 2 || cfg.Hosts[0].Label != "site" || cfg.Hosts[1].Label != "" {
		t.Errorf("hosts: %+v", cfg.Hosts)
	}
	if addr, ok := cfg.ResolveHost("site"); !ok || addr != "203.0.113.10" {
		t.Errorf("resolve by label failed")
	}

	cfg.Dev = dir
	for _, d := range []string{"projects/a/app/handbook/php", "projects/b/app"} {
		_ = os.MkdirAll(filepath.Join(dir, d), 0o755)
	}
	if p, ok := cfg.ProjectHere(filepath.Join(dir, "projects/a/app/handbook/php")); !ok || p.Name != "a-hb" {
		t.Errorf("here: want a-hb, got %v", p)
	}
	if p, ok := cfg.ProjectHere(filepath.Join(dir, "projects/a/app")); !ok || p.Name != "a" {
		t.Errorf("here: want a, got %v", p)
	}
	if _, ok := cfg.ProjectHere(os.TempDir()); ok {
		t.Error("outside must not match")
	}
}
