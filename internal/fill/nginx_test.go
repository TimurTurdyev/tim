package fill

import (
	"os"
	"testing"
)

func TestParseNginx(t *testing.T) {
	v := ParseNginx(sample(t, "nginx.conf"))
	if len(v) != 3 {
		t.Fatalf("блоков %d: %+v", len(v), v)
	}
	if v[0].TLS || v[1].Root != "/var/www/shop/current/public" || !v[1].TLS {
		t.Errorf("%+v", v)
	}
	if len(v[0].Names) != 2 || v[0].Names[0] != "shop.example.com" {
		t.Errorf("имена %v", v[0].Names)
	}
}

// sample: образец вывода утилиты из testdata.
func sample(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
