package disk

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/TimurTurdyev/tim/internal/diskuse"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/tui"
)

// findLimit: больше найденного на экране не разобрать - уточняют запрос.
const findLimit = 200

// find: места замера по словам запроса. Совпадать должны все слова - подстрокой без регистра - с именем
// папки, именем модели в кэше или подписью места ("корзина", "зависимости"). Вложенное в уже найденное
// не повторяется: найденная модель не тянет за собой свои snapshots.
func (d *state) find(q, home string) []tui.DiskRow {
	words := strings.Fields(strings.ToLower(q))
	if len(words) == 0 {
		return nil
	}
	d.mu.Lock()
	sizes := map[string]int64{}
	for p, size := range d.tree {
		if matches(p, home, words) {
			sizes[p] = size
		}
	}
	d.mu.Unlock()
	hits := make([]string, 0, len(sizes))
	for p := range sizes {
		hits = append(hits, p)
	}
	// "/" меньше любого символа: вложенное идёт сразу за своей папкой.
	sort.Slice(hits, func(i, j int) bool {
		return strings.ReplaceAll(hits[i], "/", "\x00") < strings.ReplaceAll(hits[j], "/", "\x00")
	})
	var rows []tui.DiskRow
	last := ""
	for _, p := range hits {
		if last != "" && strings.HasPrefix(p, last+"/") {
			continue
		}
		last = p
		rows = append(rows, foundRow(p, sizes[p], home))
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Size > rows[j].Size })
	return rows[:min(len(rows), findLimit)]
}

func matches(p, home string, words []string) bool {
	text := strings.ToLower(filepath.Base(p))
	if m, ok := diskuse.ModelName(p); ok {
		text += " " + strings.ToLower(m)
	}
	if l, _, ok := diskuse.LabelOf(p, home); ok {
		text += " " + strings.ToLower(l.Name)
	}
	for _, w := range words {
		if !strings.Contains(text, w) {
			return false
		}
	}
	return true
}

// foundRow: найденная папка - своё имя и где лежит: у копий одно имя, отличает их место.
func foundRow(p string, size int64, home string) tui.DiskRow {
	l, _, known := diskuse.LabelOf(p, home)
	name := filepath.Base(p)
	switch m, ok := diskuse.ModelName(p); {
	case ok:
		name = m
	case known && l.Deps:
		name += ": " + diskuse.ProjectName(p, home)
	}
	return tui.DiskRow{Name: name, Path: p, Show: paths.Show(p), Hint: paths.Show(filepath.Dir(p)), Size: size,
		Touched: diskuse.Touched(p), Removable: removable(p, home)}
}

// removable: папку можно целиком в Корзину - кэш, который вернётся сам, или своя папка в домашней. Данные
// программ, которые убирают в самой программе (Steam, Фото, UTM), и верхние папки (~/Library, ~/Dev,
// ~/Library/Caches) так не трогаем.
func removable(p, home string) bool {
	rel, err := filepath.Rel(home, p)
	if err != nil || strings.HasPrefix(rel, "..") || rel == "." {
		return false
	}
	depth := len(strings.Split(rel, "/"))
	if depth < 2 || strings.HasPrefix(rel, "Library/") && depth < 3 {
		return false
	}
	l, _, known := diskuse.LabelOf(p, home)
	return !known || l.Whole || l.Kind == diskuse.Data && !l.Stop
}
