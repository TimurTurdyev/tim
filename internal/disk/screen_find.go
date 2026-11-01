package disk

import (
	"path/filepath"
	"strings"

	"github.com/TimurTurdyev/tim/internal/diskuse"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/tui"
)

// findLimit: больше найденного на экране не разобрать - уточняют запрос.
const findLimit = 200

// find: места замера по запросу - строками экрана.
func (d *state) find(q, home string) []tui.DiskRow {
	d.mu.Lock()
	spots := diskuse.Search(d.tree, home, q, findLimit)
	d.mu.Unlock()
	rows := make([]tui.DiskRow, 0, len(spots))
	for _, s := range spots {
		rows = append(rows, foundRow(s.Path, s.Size, home))
	}
	return rows
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
