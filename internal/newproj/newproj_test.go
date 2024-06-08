package newproj

import (
	"os/exec"
	"testing"
)

func TestLayout(t *testing.T) {
	dir, parent := Layout("/r", "clients", "shop", "app")
	if dir != "/r/clients/shop/app" || parent != "/r/clients/shop" {
		t.Errorf("wrapper: %s %s", dir, parent)
	}
	dir, parent = Layout("/r", "projects", "blog", "")
	if dir != "/r/projects/blog" || parent != "/r/projects" {
		t.Errorf("plain: %s %s", dir, parent)
	}
}

// Подстановка должна пережить пробелы и кавычки в путях.
func TestExpandIsShellSafe(t *testing.T) {
	step := Expand("printf '%s|%s' {dir} {name}", "/tmp/my dir/it's", "it's", "/tmp")
	out, err := exec.Command("sh", "-c", step).Output()
	if err != nil || string(out) != "/tmp/my dir/it's|it's" {
		t.Errorf("%q -> %q %v", step, out, err)
	}
}

func TestNextPort(t *testing.T) {
	busy := map[string]bool{"8004": true}
	if p := NextPort([]int{8000, 8003, 5180}, 8000, func(s string) bool { return busy[s] }); p != 8005 {
		t.Errorf("got %d", p)
	}
	if p := NextPort(nil, 8000, func(string) bool { return false }); p != 8000 {
		t.Errorf("empty registry: %d", p)
	}
}

func TestBuiltin(t *testing.T) {
	for _, s := range Stacks() {
		if steps, ok := Builtin(s, true); !ok || len(steps) == 0 {
			t.Errorf("%s has no steps", s)
		}
	}
	if _, ok := Builtin("cobol", false); ok {
		t.Error("unknown stack")
	}
}
