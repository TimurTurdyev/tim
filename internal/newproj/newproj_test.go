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
