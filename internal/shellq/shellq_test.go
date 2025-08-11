package shellq

import (
	"os/exec"
	"strings"
	"testing"
)

func TestJoin(t *testing.T) {
	cases := map[string]string{
		"go build":                "go build",
		"echo hello world":        "echo hello world",
		"sh -c echo $HOME":        `sh -c echo '$HOME'`,
		"git commit -m сообщение": `git commit -m 'сообщение'`,
		"cp a b/c.txt":            "cp a b/c.txt",
		"grep foo|bar":            `grep 'foo|bar'`,
	}
	for in, want := range cases {
		if got := Join(strings.Split(in, " ")); got != want {
			t.Errorf("Join(%q) = %q, ожидали %q", in, got, want)
		}
	}
	// Пустой аргумент обязан остаться различимым, иначе команда поедет.
	if got := Join([]string{"ls", ""}); got != "ls ''" {
		t.Errorf("пустой аргумент: %q", got)
	}
	// Пробел внутри одного аргумента - это один аргумент в кавычках, а не два.
	if got := Join([]string{"ls", "my dir"}); got != "ls 'my dir'" {
		t.Errorf("пробел в аргументе: %q", got)
	}
}

// Значение должно дойти до настоящего sh ровно таким, какое оно у нас.
func TestQuoteRoundTrip(t *testing.T) {
	for _, v := range []string{`git status`, `echo 'hi'`, `printf "%s\n" $HOME`, "a;b && c | d", `it's "x"`, ""} {
		out, err := exec.Command("sh", "-c", "printf %s "+Quote(v)).Output()
		if err != nil || string(out) != v {
			t.Errorf("%q -> %q %v", v, out, err)
		}
	}
}
