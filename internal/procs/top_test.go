package procs

import "testing"

func TestParseTopAndGroup(t *testing.T) {
	out := `52572 1390896   1,7 /Applications/OrbStack.app/Contents/Frameworks/OrbStack Helper.app/Contents/MacOS/OrbStack Helper
12157 470224   3.3 /Applications/Firefox.app/Contents/MacOS/firefox
53469 322208   0,3 /Applications/Firefox.app/Contents/MacOS/plugin-container.app/Contents/MacOS/plugin-container
26968 499392  16,9 node
битая строка
777 abc 1 x`
	list := ParseTop(out)
	if len(list) != 4 {
		t.Fatalf("разобрано %d", len(list))
	}
	if list[0].CPU != 1.7 || list[0].RSS != 1390896*1024 {
		t.Fatalf("первая: %+v", list[0])
	}
	g := GroupBy(list, func(s Sample) string { return AppName(s.Comm) }, false)
	// OrbStack Helper внутри OrbStack.app - это OrbStack, а не отдельное приложение.
	if g[0].Name != "OrbStack" || g[1].Name != "Firefox" || g[1].Count != 2 {
		t.Fatalf("%+v", g)
	}
	byCPU := GroupBy(list, func(s Sample) string { return AppName(s.Comm) }, true)
	if byCPU[0].Name != "node" {
		t.Fatalf("по CPU первым должен быть node: %+v", byCPU)
	}
}

func TestAppName(t *testing.T) {
	cases := map[string]string{
		"/Applications/Google Chrome.app/Contents/Frameworks/Google Chrome Framework.framework/Helpers/Google Chrome Helper (Renderer).app/Contents/MacOS/Google Chrome Helper (Renderer)": "Google Chrome",
		"/usr/local/bin/php":            "php",
		"node":                          "node",
		"/System/Applications/Mail.app": "Mail",
	}
	for in, want := range cases {
		if got := AppName(in); got != want {
			t.Errorf("%s -> %s, ожидали %s", in, got, want)
		}
	}
}
