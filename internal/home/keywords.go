package home

import (
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/TimurTurdyev/tim/internal/fuzzy"
)

// keywords: слова, которыми команду ищут, а в описании их нет. Ключ - строка команды без tim.
var keywords = map[string]string{
	"port":        "порт занят занял держит освободить",
	"ports":       "порты слушает забытые серверы висят",
	"disk":        "место диск заполнен кончилось занято",
	"top":         "память тормозит жрёт ест процессор cpu",
	"hw":          "железо процессор видеокарта температура",
	"hw battery":  "батарея аккумулятор",
	"kill":        "закрыть зависло убить",
	"server":      "сервер состояние",
	"servers":     "серверы все сводка",
	"audit":       "дыры уязвимости безопасность проверить сайт",
	"site":        "сайт хостинг",
	"cert":        "сертификат ssl https истекает срок",
	"mail":        "почта письма доходят",
	"direct":      "впн vpn мимо обход напрямую мешает",
	"vpn":         "впн",
	"route":       "маршрут впн",
	"db":          "база данных sql",
	"go":          "перейти открыть проект папка",
	"logs":        "логи журнал ошибки",
	"http":        "скорость сайта грузится время ответа",
	"og":          "превью ссылки картинка телеграм",
	"ts":          "время timestamp дата unix",
	"pass":        "пароль сгенерировать",
	"today":       "сделал неделя отчёт",
	"self-update": "обновить",
	"ssh":         "зайти на сервер",
	"serve":       "запустить проект поднять",
	"check":       "проверить все проекты",
}

// stopWords: слова запроса, которые ничего не говорят о команде.
var stopWords = map[string]bool{"кто": true, "что": true, "как": true, "где": true, "мне": true, "это": true,
	"на": true, "за": true, "из": true, "по": true, "не": true, "до": true, "от": true, "во": true, "со": true, "все": true}

// searchLine: строка команды для поиска - имя, описание и слова, которых в описании нет.
func searchLine(line, desc string) string {
	return line + " " + desc + " " + keywords[line]
}

// rankCommands: сначала по основам слов (сервер и сервером одно), потом добирают совпадения по буквам -
// они ловят недописанное и опечатки.
func rankCommands(q string, names, lines []string, n int) []int {
	out := rankByWords(q, names, lines, n)
	for _, m := range fuzzy.FilterTop(q, lines, n) {
		if len(out) < n && !slices.Contains(out, m.Index) {
			out = append(out, m.Index)
		}
	}
	return out
}

// rankByWords: строки, где нашлась хотя бы половина основ запроса. Очки - совпадения в строке и ещё раз
// в имени и словах команды; при равных очках выше имя, которое начинается с запроса, потом короткое имя:
// на "di" сначала disk, а не direct check.
func rankByWords(q string, names, lines []string, n int) []int {
	want := stems(q)
	if len(want) == 0 {
		return nil
	}
	type scored struct {
		index, score, depth int
		prefix              bool
	}
	first := strings.ToLower(strings.Fields(q)[0])
	var found []scored
	for i, l := range lines {
		hits := matched(want, stems(l))
		if hits > 0 && hits*2 >= len(want) {
			key := names[i] + " " + keywords[names[i]]
			found = append(found, scored{i, hits + matched(want, stems(key)), len(strings.Fields(names[i])), strings.HasPrefix(names[i], first)})
		}
	}
	sort.SliceStable(found, func(a, b int) bool {
		x, y := found[a], found[b]
		switch {
		case x.score != y.score:
			return x.score > y.score
		case x.prefix != y.prefix:
			return x.prefix
		}
		return x.depth < y.depth
	})
	var out []int
	for _, f := range found[:min(n, len(found))] {
		out = append(out, f.index)
	}
	return out
}

// matched: сколько основ запроса есть среди основ строки; короткая основа - недописанное слово, ищется началом.
func matched(want, have []string) int {
	n := 0
	for _, w := range want {
		if slices.ContainsFunc(have, func(h string) bool { return h == w || len([]rune(w)) < 4 && strings.HasPrefix(h, w) }) {
			n++
		}
	}
	return n
}

// stems: основы слов - первые четыре буквы в нижнем регистре, ё как е: занят и занял совпадут.
func stems(s string) []string {
	var out []string
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if len([]rune(w)) < 2 || stopWords[w] {
			continue
		}
		r := []rune(strings.ReplaceAll(w, "ё", "е"))
		out = append(out, string(r[:min(4, len(r))]))
	}
	return out
}
