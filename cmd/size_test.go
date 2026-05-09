package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Файл команды - оглавление: флаги, вызов модуля, печать. Разросся - значит, в нём завелась логика,
// которой место в internal.
func TestCmdFilesStaySmall(t *testing.T) {
	const limit = 600
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if n := strings.Count(string(data), "\n"); n > limit {
			t.Errorf("%s: %d строк, больше %d - вынеси логику в internal", f, n, limit)
		}
	}
}
