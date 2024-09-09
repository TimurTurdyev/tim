// Package fuzzy: ранжирование строк по подпоследовательности символов запроса.
package fuzzy

import (
	"sort"
	"unicode"
)

type Match struct {
	Index     int
	Score     int
	Positions []int
	length    int
}

const (
	scoreChar     = 1
	bonusFirst    = 8
	bonusBoundary = 6
	bonusRun      = 4
	bonusSubstr   = 10
)

// Filter оставляет строки, где руны запроса идут по порядку; при равных очках короче выше.
// Пустой запрос сохраняет исходный порядок.
func Filter(query string, items []string) []Match {
	q := []rune(lower(query))
	out := make([]Match, 0, len(items))
	for i, s := range items {
		if len(q) == 0 {
			out = append(out, Match{Index: i})
			continue
		}
		if m, ok := match(q, []rune(s)); ok {
			m.Index = i
			out = append(out, m)
		}
	}
	if len(q) == 0 {
		return out
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].Score != out[b].Score {
			return out[a].Score > out[b].Score
		}
		if out[a].length != out[b].length {
			return out[a].length < out[b].length
		}
		return out[a].Index < out[b].Index
	})
	return out
}

// match пробует каждое вхождение первой руны и берёт лучший жадный проход от него.
func match(q, s []rune) (Match, bool) {
	ls := []rune(lower(string(s)))
	best := Match{Score: -1}
	for start := range ls {
		if ls[start] != q[0] {
			continue
		}
		m, ok := greedy(q, ls, start)
		if ok && m.Score > best.Score {
			best = m
		}
	}
	if best.Score < 0 {
		return Match{}, false
	}
	best.length = len(s)
	return best, true
}

func greedy(q, s []rune, start int) (Match, bool) {
	pos := make([]int, 0, len(q))
	score := 0
	qi := 0
	for i := start; i < len(s) && qi < len(q); i++ {
		if s[i] != q[qi] {
			continue
		}
		score += scoreChar
		switch {
		case i == 0:
			score += bonusFirst
		case isBoundary(s[i-1]):
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

func isBoundary(r rune) bool {
	return r == '/' || r == '-' || r == '_' || r == '.' || r == '@' || unicode.IsSpace(r)
}

func lower(s string) string {
	r := []rune(s)
	for i, c := range r {
		r[i] = unicode.ToLower(c)
	}
	return string(r)
}
