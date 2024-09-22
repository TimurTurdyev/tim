package envfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := "A=1\nB=\"two words\" # c\nC='x # not comment'\nD=raw # comment\nE=\nexport F=yes\r\nA=2\n# G=skip\nURL=http://localhost:8003\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	vals, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{"A": "2", "B": "two words", "C": "x # not comment", "D": "raw", "E": "", "F": "yes"}
	for k, want := range cases {
		if got := vals[k]; got != want {
			t.Errorf("%s: want [%s], got [%s]", k, want, got)
		}
	}
	if _, ok := vals["G"]; ok {
		t.Error("commented key must be absent")
	}
	if got := Port(vals["URL"]); got != "8003" {
		t.Errorf("port: want 8003, got %s", got)
	}
	if got := Port("http://localhost"); got != "" {
		t.Errorf("port without number: want empty, got %s", got)
	}
}

func TestMask(t *testing.T) {
	cases := []struct{ key, val, want string }{
		{"DB_PASSWORD", "hunter2", "***"},
		{"APP_KEY", "base64:abc", "***"},
		{"STRIPE_SECRET", "sk_live", "***"},
		{"DB_HOST", "127.0.0.1", "127.0.0.1"},
		{"DATABASE_URL", "postgres://app:hunter2@db:5432/shop?sslmode=disable", "postgres://app:***@db:5432/shop?sslmode=disable"},
		{"REDIS_URL", "redis://db:6379", "redis://db:6379"},
		{"DB_PASSWORD", "", ""},
	}
	for _, c := range cases {
		if got := Mask(c.key, c.val); got != c.want {
			t.Errorf("%s=%s: %s", c.key, c.val, got)
		}
	}
}
