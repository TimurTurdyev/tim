package vulns

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestLockfiles(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"composer.lock": `{"packages":[{"name":"guzzlehttp/guzzle","version":"7.4.1"},{"name":"acme/x","version":"dev-main"}],
			"packages-dev":[{"name":"phpunit/phpunit","version":"v10.5.0"}]}`,
		"package-lock.json": `{"lockfileVersion":3,"packages":{"":{"name":"shop"},"node_modules/axios":{"version":"1.6.0"},
			"node_modules/a/node_modules/axios":{"version":"0.21.1"}}}`,
		"go.mod": "module x\n\nrequire golang.org/x/net v0.10.0\n\nrequire (\n\tgithub.com/spf13/cobra v1.8.0 // indirect\n)\n",
	}
	for n, b := range files {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(b), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pkgs, _ := Lockfiles(dir)
	var got []string
	for _, p := range pkgs {
		got = append(got, p.Ecosystem+" "+p.Name+" "+p.Version)
	}
	slices.Sort(got)
	want := []string{"Go github.com/spf13/cobra v1.8.0", "Go golang.org/x/net v0.10.0", "Packagist guzzlehttp/guzzle 7.4.1",
		"Packagist phpunit/phpunit 10.5.0", "npm axios 0.21.1", "npm axios 1.6.0"}
	if !slices.Equal(got, want) {
		t.Fatalf("%q", got)
	}
	if !newer("7.4.5", "7.4.1") || newer("7.4.1", "7.10.0") {
		t.Fatal("сравнение версий")
	}
}
