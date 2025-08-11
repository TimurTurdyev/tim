package newproj

import (
	"os/exec"
	"testing"
)

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
