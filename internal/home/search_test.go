package home

import (
	"os"
	"strings"
	"testing"
)

// Фразы, которыми ищут команду, и команда, которая должна быть среди первых четырёх. Список команд -
// снимок дерева tim в testdata: веса поиска легко незаметно сдвинуть, таблица это ловит.
func TestRankCommands(t *testing.T) {
	data, err := os.ReadFile("testdata/commands.tsv")
	if err != nil {
		t.Fatal(err)
	}
	var names, lines []string
	for _, l := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		name, desc, _ := strings.Cut(l, "\t")
		names = append(names, name)
		lines = append(lines, searchLine(name, desc))
	}
	cases := [][2]string{
		{"порт занят", "port"}, {"кто занял порт", "port"}, {"место на диске", "disk"}, {"диск заполнен", "disk"},
		{"кто ест память", "top"}, {"тормозит", "top"}, {"пароль", "pass"}, {"сертификат", "cert"},
		{"сертификат истекает", "cert"}, {"что с сервером", "server"}, {"дыры на сайте", "audit"}, {"проверить сайт", "audit"},
		{"почта не доходит", "mail"}, {"dns", "dns"}, {"мимо впн", "direct"}, {"впн мешает", "direct"},
		{"туннель к базе", "tunnel db"}, {"база данных", "db"}, {"открыть проект", "go"}, {"запустить проект", "serve"},
		{"логи", "logs"}, {"забытые серверы", "ports"}, {"закрыть приложение", "kill"}, {"батарея", "hw battery"},
		{"wifi", "net wifi"}, {"внешний ip", "ip"}, {"скорость сайта", "http"}, {"превью ссылки", "og"},
		{"cors", "cors"}, {"версия php", "php"}, {"uuid", "uuid"}, {"jwt", "jwt"}, {"base64", "b64"},
		{"время из timestamp", "ts"}, {"что сделал за неделю", "today"}, {"обновить tim", "self-update"},
	}
	for _, c := range cases {
		var got []string
		found := false
		for _, i := range rankCommands(c[0], names, lines, 4) {
			got = append(got, names[i])
			found = found || names[i] == c[1]
		}
		if !found {
			t.Errorf("%q: нет %s среди %v", c[0], c[1], got)
		}
	}
}
