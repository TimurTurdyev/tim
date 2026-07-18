package diskuse

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSummarizeSystemNoDoubleCount(t *testing.T) {
	places := []Place{
		{Name: "Homebrew", Paths: []string{"Library/Caches/Homebrew"}},
		{Name: "npm", Paths: []string{".npm"}},
		{Name: "пусто", Paths: []string{"nothing"}},
	}
	sizes := map[string]int64{"Library/Caches": 10 << 30, "Library/Caches/Homebrew": 4 << 30, ".npm": 1 << 30}
	got := SummarizeSystem(places, sizes)
	if len(got) != 3 {
		t.Fatalf("%+v", got)
	}
	// Caches 10 ГБ, из них 4 - Homebrew: прочего остаётся 6, а не 10.
	if got[0].Name != "~/Library/Caches: прочее" || got[0].Size != 6<<30 || got[1].Name != "Homebrew" {
		t.Fatalf("%+v", got)
	}
}

func TestOldIDEVersions(t *testing.T) {
	old := OldIDEVersions([]string{"GoLand2024.2", "GoLand2025.2", "PhpStorm2025.2", "PhpStorm2025.3",
		"PyCharm2024.3", "Toolbox", "Daemon", "acp-agents"})
	if !old["GoLand2024.2"] || !old["PhpStorm2025.2"] || len(old) != 2 {
		t.Fatalf("%v", old)
	}
	// Единственная версия продукта не старая, даже если давняя: это могла быть текущая.
	if old["PyCharm2024.3"] {
		t.Fatal("PyCharm без более новой версии помечен старым")
	}
}

func TestParseDuChildren(t *testing.T) {
	out := "10\t/h/.cache/uv\n20\t/h/.cache/huggingface\n35\t/h/.cache\ndu: /h/.cache/x: Permission denied\n5\t/other/y\n"
	sizes := ParseDu(out, "/h")
	if sizes[".cache"] != 35*1024 || len(sizes) != 3 {
		t.Fatalf("%v", sizes)
	}
	if ch := Children(sizes, ".cache"); len(ch) != 2 || ch[0].Path != "huggingface" {
		t.Errorf("%v", ch)
	}
}

func TestSpotsDrillDown(t *testing.T) {
	const g = 1 << 30
	tree := Tree{
		"/h": 300 * g, "/h/Library": 200 * g, "/h/Library/Steam": 150 * g,
		"/h/Library/Steam/common": 150 * g, "/h/Library/Steam/common/dota": 90 * g, "/h/Library/Steam/common/rust": 60 * g,
		"/h/Library/Other": 50 * g, "/h/Dev": 100 * g, "/h/Dev/a": 2 * g, "/h/Dev/a/node_modules": 2 * g,
	}
	stop := func(p string) bool { return filepath.Base(p) == "node_modules" }
	got := Spots(tree, "/h", g, stop)
	want := []Spot{
		{Path: "/h/Dev", Size: 98 * g, Rest: true}, {Path: "/h/Library/Steam/common/dota", Size: 90 * g},
		{Path: "/h/Library/Steam/common/rust", Size: 60 * g}, {Path: "/h/Library/Other", Size: 50 * g},
		{Path: "/h/Dev/a/node_modules", Size: 2 * g},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%+v", got)
	}
}

func TestParseACF(t *testing.T) {
	g := ParseACF("\"AppState\"\n{\n\t\"name\"\t\t\"Dota 2\"\n\t\"installdir\"\t\t\"dota 2 beta\"\n\t\"LastPlayed\"\t\t\"1780701016\"\n}")
	if g.Name != "Dota 2" || g.Dir != "dota 2 beta" || g.LastPlayed.Unix() != 1780701016 {
		t.Errorf("acf %+v", g)
	}
}

func TestParseAPFS(t *testing.T) {
	out := sample(t, "diskutil-apfs-list.txt")
	v := ParseAPFS(out, "/System/Volumes/Data")
	if len(v) != 3 || v[0].Role != "System" || v[2].Name != "old-backup" || v[2].Size != 41364611072 || v[1].Mount != "/System/Volumes/Data" {
		t.Errorf("%+v", v)
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
