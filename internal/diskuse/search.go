package diskuse

import (
	"cmp"
	"path/filepath"
	"slices"
	"strings"

	"github.com/TimurTurdyev/tim/internal/cache"
)

// Search: папки замера по словам запроса. Совпадать должны все слова - подстрокой без регистра - с именем
// папки, именем модели в кэше или подписью места ("корзина", "зависимости"). Вложенное в уже найденное
// не повторяется: найденная модель не тянет за собой свои папки. Крупные первыми, не больше limit.
func Search(tree Tree, home, q string, limit int) []Spot {
	words := strings.Fields(strings.ToLower(q))
	if len(words) == 0 {
		return nil
	}
	var hits []Spot
	for p, size := range tree {
		if matches(p, home, words) {
			hits = append(hits, Spot{Path: p, Size: size})
		}
	}
	// "/" меньше любого символа: вложенное идёт сразу за своей папкой.
	slices.SortFunc(hits, func(a, b Spot) int {
		return strings.Compare(strings.ReplaceAll(a.Path, "/", "\x00"), strings.ReplaceAll(b.Path, "/", "\x00"))
	})
	var out []Spot
	last := ""
	for _, h := range hits {
		if last != "" && strings.HasPrefix(h.Path, last+"/") {
			continue
		}
		last = h.Path
		out = append(out, h)
	}
	slices.SortStableFunc(out, func(a, b Spot) int { return cmp.Compare(b.Size, a.Size) })
	return out[:min(len(out), limit)]
}

func matches(p, home string, words []string) bool {
	text := strings.ToLower(filepath.Base(p))
	if m, ok := ModelName(p); ok {
		text += " " + strings.ToLower(m)
	}
	if l, _, ok := LabelOf(p, home); ok {
		text += " " + strings.ToLower(l.Name)
	}
	for _, w := range words {
		if !strings.Contains(text, w) {
			return false
		}
	}
	return true
}

// Cached: последний замер из кэша, сколько бы ему ни было, - для поиска с главного без нового замера.
func Cached() (Tree, bool) {
	c, ok := cache.Load[measured](CachePath())
	if !ok {
		return nil, false
	}
	return c.Value.Tree, true
}
