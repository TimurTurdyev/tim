package cmd

import "testing"

func TestTranslateError(t *testing.T) {
	cases := map[string]string{
		"unknown command \"serv\" for \"tim\"\n\nDid you mean this?\n\tserve\n\tsvc\n": "нет команды serv у tim: tim --help; может, serve или svc?",
		"unknown flag: --nope":                        "нет флага --nope, посмотри --help",
		"unknown shorthand flag: 'q' in -q":           "нет флага -q, посмотри --help",
		"requires at least 1 arg(s), only received 0": "нужно аргументов не меньше 1, передано 0",
		"accepts at most 2 arg(s), received 3":        "аргументов не больше 2, передано 3",
		"invalid argument \"x\" for \"--port\" flag: strconv.ParseInt: parsing \"x\": invalid syntax": "неверное значение x для --port",
		"своя ошибка tim": "своя ошибка tim",
	}
	for in, want := range cases {
		if got := translateError(in); got != want {
			t.Errorf("%q:\n got  %s\n want %s", in, got, want)
		}
	}
}

func TestBestSuggestions(t *testing.T) {
	got := bestSuggestions("prt", []string{"cert", "jwt", "net", "php", "port", "ports", "proj", "qr", "url"}, 3)
	if got[0] != "port" || len(got) != 3 {
		t.Errorf("prt: %v", got)
	}
	if got := bestSuggestions("serv", []string{"cert", "env", "serve"}, 3); got[0] != "serve" {
		t.Errorf("prefix first: %v", got)
	}
}
