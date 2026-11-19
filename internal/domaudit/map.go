package domaudit

import (
	_ "embed"
	"encoding/json"
	"html"
	"os"
	"strings"
	"time"
)

// page: страница домена. Рисует всё сама, внешних библиотек и обращений в сеть нет.
//
//go:embed page.html
var page string

// WriteMap собирает самодостаточную HTML-страницу: сводка, карта и таблицы.
func WriteMap(rep *Report, path string) error {
	type seen struct {
		Ports   []int    `json:"ports"`
		Answers []string `json:"answers"`
	}
	ports := map[string]seen{}
	for _, a := range rep.Addrs {
		ports[a.IP] = seen{a.Ports(), a.Answers()}
	}
	data, err := json.Marshal(struct {
		*Report
		Networks  []Network       `json:"networks"`
		Countries []Country       `json:"countries"`
		Seen      map[string]seen `json:"seen"`
		Made      time.Time       `json:"made"`
	}{rep, Networks(rep), Countries(rep), ports, time.Now()})
	if err != nil {
		return err
	}
	// json.Marshal пишет < > & кодами, поэтому закрыть тег данными нельзя.
	out := strings.Replace(page, "__TITLE__", html.EscapeString(rep.Domain), 1)
	out = strings.Replace(out, "__DATA__", string(data), 1)
	return os.WriteFile(path, []byte(out), 0o644)
}
