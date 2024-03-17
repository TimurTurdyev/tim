package release

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"strings"
	"testing"
)

func TestPickAsset(t *testing.T) {
	assets := []Asset{
		{Name: "checksums.txt"},
		{Name: "tim_1.2.0_darwin_all.tar.gz"},
		{Name: "tim_1.2.0_linux_amd64.tar.gz"},
	}
	if a, ok := PickAsset(assets, "darwin", "arm64"); !ok || a.Name != "tim_1.2.0_darwin_all.tar.gz" {
		t.Errorf("universal: %+v %v", a, ok)
	}
	assets = append(assets, Asset{Name: "tim_1.2.0_darwin_arm64.tar.gz"})
	if a, _ := PickAsset(assets, "darwin", "arm64"); a.Name != "tim_1.2.0_darwin_arm64.tar.gz" {
		t.Errorf("exact arch should win: %+v", a)
	}
	if _, ok := PickAsset(assets, "windows", "amd64"); ok {
		t.Error("no windows asset")
	}
}

func TestParseChecksums(t *testing.T) {
	sum := strings.Repeat("a", 64)
	m := ParseChecksums(strings.NewReader(sum + "  tim_1.2.0_darwin_all.tar.gz\nbroken line\n" + strings.Repeat("B", 64) + " *x.tar.gz\n"))
	if m["tim_1.2.0_darwin_all.tar.gz"] != sum || m["x.tar.gz"] != strings.Repeat("b", 64) || len(m) != 2 {
		t.Errorf("%v", m)
	}
}

func TestNewer(t *testing.T) {
	cases := []struct {
		l, c string
		want bool
	}{
		{"v1.2.0", "v1.1.9", true},
		{"v1.2.0", "1.2.0", false},
		{"v1.10.0", "v1.9.3", true},
		{"v1.2.0", "dev", true},
		{"v1.2.0", "0b4497d", true},
		{"v1.2.0", "v1.2.1-3-gabc-dirty", false},
	}
	for _, c := range cases {
		if got := Newer(c.l, c.c); got != c.want {
			t.Errorf("%s vs %s: %v", c.l, c.c, got)
		}
	}
}

func TestExtractTar(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range map[string]string{"README.md": "readme", "tim": "binary"} {
		_ = tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte(body))
	}
	_ = tw.Close()
	_ = gz.Close()
	data := buf.Bytes()
	got, err := ExtractTar(bytes.NewReader(data), "tim")
	if err != nil || string(got) != "binary" {
		t.Errorf("%q %v", got, err)
	}
	if _, err := ExtractTar(bytes.NewReader(data), "nope"); err == nil {
		t.Error("missing file should fail")
	}
}
