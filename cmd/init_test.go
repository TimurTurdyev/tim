package cmd

import (
	"os/exec"
	"testing"
)

// Значение алиаса должно дойти до shell ровно таким, какое оно в конфиге.
func TestShellQuoteRoundTrip(t *testing.T) {
	for _, v := range []string{`git status`, `echo 'hi'`, `printf "%s\n" $HOME`, "a;b && c | d", `it's "x"`} {
		out, err := exec.Command("sh", "-c", "printf %s "+shellQuote(v)).Output()
		if err != nil || string(out) != v {
			t.Errorf("%q -> %q %v", v, out, err)
		}
	}
	if validAlias("rm -rf") || validAlias("tim") || !validAlias("gs") {
		t.Error("alias names")
	}
}
