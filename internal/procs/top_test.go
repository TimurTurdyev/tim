package procs

import "testing"

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
