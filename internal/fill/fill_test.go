package fill

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, dir, body string) string {
	t.Helper()
	d := filepath.Join(dir, "p")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, ".env"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestFromEnv(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want Found
	}{
		{"сайт с портом", "APP_URL=http://shop.example.com:8003\nDB_CONNECTION=pgsql\nDB_DATABASE=shop\n",
			Found{URL: "http://shop.example.com:8003", Port: 8003, DB: "pgsql:shop"}},
		{"боевой без порта", "APP_URL=https://example.com\nDB_CONNECTION=mysql\nDB_DATABASE=main\n",
			Found{URL: "https://example.com", DB: "mysql:main"}},
		{"localhost не адрес сайта", "APP_URL=http://localhost:8000\nDB_CONNECTION=mysql\nDB_DATABASE=app\n",
			Found{Port: 8000, DB: "mysql:app"}},
		{"домен .test пропускаем", "APP_URL=http://shop.test\n", Found{}},
		{"база без типа", "DB_DATABASE=solo\n", Found{DB: "solo"}},
		{"пусто", "APP_NAME=x\n", Found{}},
		{"кавычки и комментарий", "APP_URL=\"https://a.example.com\" # прод\n", Found{URL: "https://a.example.com"}},
	}
	for _, c := range cases {
		dir := write(t, t.TempDir(), c.env)
		if got := FromEnv(dir); got != c.want {
			t.Errorf("%s: %+v want %+v", c.name, got, c.want)
		}
	}
	if got := FromEnv(filepath.Join(t.TempDir(), "нет")); got != (Found{}) {
		t.Errorf("без .env: %+v", got)
	}
}

func TestSqlitePath(t *testing.T) {
	root := t.TempDir()
	dir := write(t, root, "DB_CONNECTION=sqlite\nDB_DATABASE="+filepath.Join(root, "p", "database", "db.sqlite")+"\n")
	if got := FromEnv(dir).DB; got != "sqlite:database/db.sqlite" {
		t.Errorf("sqlite: %q", got)
	}
	outside := write(t, t.TempDir(), "DB_CONNECTION=sqlite\nDB_DATABASE=/var/db/other.sqlite\n")
	if got := FromEnv(outside).DB; got != "sqlite:/var/db/other.sqlite" {
		t.Errorf("вне проекта: %q", got)
	}
}
