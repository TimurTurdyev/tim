package procs

import "testing"

func TestGroupBy(t *testing.T) {
	list := []Sample{
		{PID: "52572", RSS: 1390896 * 1024, CPU: 1.7, Comm: "/Applications/OrbStack.app/Contents/Frameworks/OrbStack Helper.app/Contents/MacOS/OrbStack Helper"},
		{PID: "12157", RSS: 470224 * 1024, CPU: 3.3, Comm: "/Applications/Firefox.app/Contents/MacOS/firefox"},
		{PID: "53469", RSS: 322208 * 1024, CPU: 0.3, Comm: "/Applications/Firefox.app/Contents/MacOS/plugin-container.app/Contents/MacOS/plugin-container"},
		{PID: "26968", RSS: 499392 * 1024, CPU: 16.9, Comm: "node"},
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
