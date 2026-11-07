// Package sitecheck: проверка сайта снаружи - то же, что tim audit, для других программ (страница
// проверки на timcode.ru). Отдаёт report.Report: в Data - разбор для JSON, в блоках - находки.
package sitecheck

import (
	"errors"
	"fmt"
	"strings"

	"github.com/TimurTurdyev/tim/internal/audit"
	"github.com/TimurTurdyev/tim/internal/webnet"
	"github.com/TimurTurdyev/tim/report"
)

// Options: Intrusive - ещё пути вроде /.env и порты баз, это для владельца сайта; CacheDir - где хранить
// сроки версий с endoflife.date, пусто - каталог состояния tim.
type Options = audit.Options

// Net: как ходить к проверяемому сайту; Control видит каждое соединение.
type Net = webnet.Net

// UseNet ставит сеть для всех проверок; зовётся один раз до первой проверки.
func UseNet(n Net) { webnet.Use(n) }

// Target: адрес сайта по тому, что ввёл человек. Домен без схемы - https.
func Target(arg string) (string, error) {
	arg = strings.TrimSpace(arg)
	switch {
	case strings.Contains(arg, "://"):
		return arg, nil
	case strings.Contains(arg, "."):
		return "https://" + arg, nil
	case arg == "":
		return "", errors.New("сайт не указан")
	}
	return "", fmt.Errorf("%s - не домен", arg)
}

// Check проверяет сайт по адресу из Target. Вердикт - рядом с адресом, находки - худшие сверху.
func Check(target string, o Options) (report.Report, error) {
	r, err := audit.Run(target, o)
	if err != nil {
		return report.Report{}, err
	}
	return report.Report{Schema: "tim.audit/1", Title: r.Final, Data: r,
		Verdict: report.Verdict{Level: r.Verdict(), Text: verdictText(r.Findings)},
		Blocks:  []report.Block{report.Findings(r.Findings), report.Note("проверено: " + strings.Join(r.Checked, ", "))}}, nil
}

func verdictText(list []report.Finding) string {
	var bad, warn int
	for _, f := range list {
		switch f.Level {
		case report.Bad:
			bad++
		case report.Warn:
			warn++
		}
	}
	switch {
	case bad > 0:
		return fmt.Sprintf("плохо: %d %s", bad, plural(bad, "серьёзная находка", "серьёзные находки", "серьёзных находок"))
	case warn > 0:
		return fmt.Sprintf("есть что поправить: %d", warn)
	}
	return "в порядке"
}

// plural: форма слова для числа - 1 находка, 2 находки, 5 находок.
func plural(n int, one, few, many string) string {
	n %= 100
	switch {
	case n%10 == 1 && n != 11:
		return one
	case n%10 >= 2 && n%10 <= 4 && (n < 12 || n > 14):
		return few
	}
	return many
}
