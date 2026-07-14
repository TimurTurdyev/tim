// Package fuzzy: ранжирование строк по подпоследовательности символов запроса.
package fuzzy

import (
	"cmp"
	"slices"
	"strings"
	"unicode"
)

const (
	scoreChar     = 1
	bonusFirst    = 8
	bonusBoundary = 6
	bonusRun      = 4
	bonusSubstr   = 10
)

// Match: строка items[Index], её очки и позиции совпавших рун для подсветки.
type Match struct {
	Index     int
	Score     int
	Positions []int
	length    int
}

// Filter оставляет строки, где есть все слова запроса, у каждого руны идут по порядку; при равных
// очках короче выше. Пустой запрос сохраняет исходный порядок.
func Filter(query string, items []string) []Match {
	words := queryWords(query)
	out := make([]Match, 0, len(items))
	for i, s := range items {
		if len(words) == 0 {
			out = append(out, Match{Index: i})
			continue
		}
		if m, ok := matchAll(words, []rune(s)); ok {
			m.Index = i
			out = append(out, m)
		}
	}
	if len(words) == 0 {
		return out
	}
	slices.SortStableFunc(out, func(a, b Match) int {
		return cmp.Or(cmp.Compare(b.Score, a.Score), cmp.Compare(a.length, b.length), cmp.Compare(a.Index, b.Index))
	})
	return out
}

// FilterTop: как Filter, но не больше n лучших.
func FilterTop(query string, items []string, n int) []Match {
	out := Filter(query, items)
	return out[:min(n, len(out))]
}

// MinScore: меньше этого - буквы запроса разбросаны по строке, такое совпадение скорее шум.
func MinScore(query string) int {
	n := 0
	for _, w := range queryWords(query) {
		n += len(w)
	}
	return 2 * n
}

func queryWords(query string) [][]rune {
	var words [][]rune
	for _, w := range strings.Fields(strings.ToLower(query)) {
		words = append(words, []rune(w))
	}
	return words
}

// matchAll: каждое слово ищется во всей строке; очки складываются, позиции объединяются.
func matchAll(words [][]rune, s []rune) (Match, bool) {
	ls := lowerRunes(s)
	var all Match
	for _, w := range words {
		m, ok := match(w, s, ls)
		if !ok {
			return Match{}, false
		}
		all.Score += m.Score
		all.Positions = append(all.Positions, m.Positions...)
	}
	if len(words) > 1 {
		slices.Sort(all.Positions)
		all.Positions = slices.Compact(all.Positions)
	}
	all.length = len(s)
	return all, true
}

// match пробует каждое вхождение первой руны и берёт лучший жадный проход от него.
func match(q, s, ls []rune) (Match, bool) {
	if !subsequence(q, ls) {
		return Match{}, false
	}
	best := Match{Score: -1}
	for start := range ls {
		if ls[start] != q[0] {
			continue
		}
		m, ok := greedy(q, s, ls, start)
		if ok && m.Score > best.Score {
			best = m
		}
	}
	return best, best.Score >= 0
}

// subsequence: дешёвая проверка до перебора стартов - большинство строк отсеивается здесь.
func subsequence(q, s []rune) bool {
	qi := 0
	for _, r := range s {
		if r == q[qi] {
			qi++
			if qi == len(q) {
				return true
			}
		}
	}
	return false
}

func greedy(q, s, ls []rune, start int) (Match, bool) {
	pos := make([]int, 0, len(q))
	score := 0
	qi := 0
	for i := start; i < len(ls) && qi < len(q); i++ {
		if ls[i] != q[qi] {
			continue
		}
		score += scoreChar
		switch {
		case i == 0:
			score += bonusFirst
		case isBoundary(s[i-1], s[i]):
			score += bonusBoundary
		}
		if len(pos) > 0 && pos[len(pos)-1] == i-1 {
			score += bonusRun
		}
		pos = append(pos, i)
		qi++
	}
	if qi < len(q) {
		return Match{}, false
	}
	if pos[len(pos)-1]-pos[0] == len(pos)-1 {
		score += bonusSubstr
	}
	return Match{Score: score, Positions: pos}, true
}

// isBoundary: начало слова - после разделителя или заглавная после строчной (ProjectHere).
func isBoundary(prev, cur rune) bool {
	switch prev {
	case '/', '-', '_', '.', '@', ':':
		return true
	}
	return unicode.IsSpace(prev) || unicode.IsLower(prev) && unicode.IsUpper(cur)
}

func lowerRunes(s []rune) []rune {
	out := make([]rune, len(s))
	for i, c := range s {
		out[i] = unicode.ToLower(c)
	}
	return out
}
