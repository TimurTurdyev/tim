package rcfile

import (
	"path/filepath"
	"testing"
)

// Регрессия: при $SHELL=/bin/sh строка tim init уходила в ~/.zshrc, который такая оболочка не читает.
func TestPathByShell(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cases := []struct {
		shell, file string
		bad         bool
	}{
		{"/bin/zsh", ".zshrc", false},
		{"/bin/bash", ".bashrc", false},
		{"/bin/sh", "", true},
		{"/usr/bin/fish", "", true},
	}
	for _, c := range cases {
		t.Setenv("SHELL", c.shell)
		path, _, err := Path("")
		switch {
		case c.bad && err == nil:
			t.Errorf("%s: ждали ошибку, получили %s", c.shell, path)
		case !c.bad && (err != nil || path != filepath.Join(home, c.file)):
			t.Errorf("%s: %s %v, ждали %s", c.shell, path, err, c.file)
		}
	}
}
