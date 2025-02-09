package diskuse

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// Place: место, где на машине разработчика копится пересоздаваемое.
type Place struct {
	Name  string
	Paths []string // от домашней папки
	Clean string   // чем чистить; пусто - разбирать руками
}

// SystemPlaces: известные места. Порядок не важен, вывод сортируется по размеру.
var SystemPlaces = []Place{
	{"OrbStack", []string{"Library/Group Containers/HUAQ24HBR6.dev.orbstack", ".orbstack"}, "docker system prune -a"},
	{"Docker Desktop", []string{"Library/Containers/com.docker.docker"}, "docker system prune -a; не пользуешься - удалить само приложение"},
	{"npm", []string{".npm"}, "npm cache clean --force"},
	{"composer", []string{"Library/Caches/composer", ".composer/cache"}, "composer clear-cache"},
	{"Homebrew", []string{"Library/Caches/Homebrew"}, "brew cleanup --prune=all"},
	{"go: модули", []string{"go/pkg/mod"}, "go clean -modcache"},
	{"go: сборка", []string{"Library/Caches/go-build"}, "go clean -cache"},
	{"pip", []string{"Library/Caches/pip"}, "pip cache purge"},
	{"bun", []string{".bun/install/cache"}, "bun pm cache rm"},
	{"pnpm", []string{"Library/pnpm/store", "Library/Caches/pnpm"}, "pnpm store prune"},
	{"yarn", []string{"Library/Caches/Yarn"}, "yarn cache clean"},
	{"JetBrains: кеши IDE", []string{"Library/Caches/JetBrains"}, "папки прошлых версий IDE удаляются целиком, текущую не трогать"},
	{"Xcode DerivedData", []string{"Library/Developer/Xcode/DerivedData"}, "rm -rf ~/Library/Developer/Xcode/DerivedData"},
	{"VS Code: кеш", []string{"Library/Application Support/Code/Cache", "Library/Application Support/Code/CachedData"}, "удалить при закрытом редакторе"},
	{"~/.cache", []string{".cache"}, ""},
	{"Корзина", []string{".Trash"}, "очистить Корзину в Finder"},
	{"Загрузки", []string{"Downloads"}, ""},
}

// Caches: папка общих кешей; из неё вычитается то, что показано отдельной строкой.
const Caches = "Library/Caches"

// Measured: место с размером в байтах.
type Measured struct {
	Place
	Size int64
}

// Summarize считает "прочее" в Caches без вложенных мест и сортирует по размеру.
// sizes - размер каждого пути из Paths и Caches, от домашней папки.
func SummarizeSystem(places []Place, sizes map[string]int64) []Measured {
	var out []Measured
	var nested int64
	for _, p := range places {
		var size int64
		for _, path := range p.Paths {
			size += sizes[path]
			if strings.HasPrefix(path, Caches+"/") {
				nested += sizes[path]
			}
		}
		if size > 0 {
			out = append(out, Measured{Place: p, Size: size})
		}
	}
	if rest := sizes[Caches] - nested; rest > 0 {
		out = append(out, Measured{Place: Place{Name: "~/Library/Caches: прочее", Paths: []string{Caches},
			Clean: "приложения пересоздадут сами; чистить при закрытых приложениях"}, Size: rest})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Size > out[j].Size })
	return out
}

// AllPaths: все пути, которые надо измерить.
func AllPaths(places []Place) []string {
	seen := map[string]bool{Caches: true}
	out := []string{Caches}
	for _, p := range places {
		for _, path := range p.Paths {
			if !seen[path] {
				seen[path] = true
				out = append(out, filepath.Clean(path))
			}
		}
	}
	return out
}

// OldIDEVersions: из папок вида GoLand2024.2 - те, у которых есть более новая версия того же продукта.
// Папки без версии (Toolbox, Daemon) не трогаются.
func OldIDEVersions(names []string) map[string]bool {
	type ver struct {
		name string
		y, m int
	}
	newest := map[string]ver{}
	parsed := map[string]ver{}
	for _, n := range names {
		i := strings.IndexFunc(n, func(r rune) bool { return r >= '0' && r <= '9' })
		if i <= 0 {
			continue
		}
		var y, m int
		if _, err := fmt.Sscanf(n[i:], "%d.%d", &y, &m); err != nil || y < 2000 {
			continue
		}
		v := ver{n, y, m}
		parsed[n] = v
		prod := n[:i]
		if cur, ok := newest[prod]; !ok || y > cur.y || y == cur.y && m > cur.m {
			newest[prod] = v
		}
	}
	old := map[string]bool{}
	for n, v := range parsed {
		i := strings.IndexFunc(n, func(r rune) bool { return r >= '0' && r <= '9' })
		if newest[n[:i]] != v {
			old[n] = true
		}
	}
	return old
}
