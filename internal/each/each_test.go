package each

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOlder(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"2.18.3", "2.19.0", true},
		{"2.19.0", "2.19.0", false},
		{"2.19.1", "2.19.0", false},
		{"2.9", "2.10.0", true},
		{"v2.19.0-beta.1", "2.19.0", false},
		{"tool version 1.4.2 (darwin)", "1.5", true},
		{"", "2.19.0", true},
	}
	for _, c := range cases {
		if got := Older(c.a, c.b); got != c.want {
			t.Errorf("%s < %s: %v", c.a, c.b, got)
		}
	}
}

func TestLocateScanVersion(t *testing.T) {
	root := t.TempDir()
	const marker = "tool.json"
	for _, d := range []string{"shop", "blog/app", "client/x/node_modules/y", "deep/a/b/c/d/e"} {
		dir := filepath.Join(root, d)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, marker), []byte(`{"version":"2.18.0","n":1}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	shop := filepath.Join(root, "shop")
	got := Locate([]string{shop, shop + "/", filepath.Join(root, "blog"), filepath.Join(root, "blog/app")}, marker)
	if len(got) != 2 || got[0] != shop {
		t.Errorf("locate %v", got)
	}
	if v := JSONVersion(filepath.Join(shop, marker), "version"); v != "2.18.0" {
		t.Errorf("version %q", v)
	}
	if v := JSONVersion(filepath.Join(shop, marker), "n"); v != "" {
		t.Errorf("non-string key %q", v)
	}
	if scan := Scan(root, marker, 4); len(scan) != 2 {
		t.Errorf("scan %v", scan)
	}
}
