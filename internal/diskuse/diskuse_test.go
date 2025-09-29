package diskuse

import "testing"

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
