package diskuse

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// SystemPlaces: известные места. Порядок не важен, вывод сортируется по размеру.
var SystemPlaces = []Place{
	{"OrbStack", []string{"Library/Group Containers/HUAQ24HBR6.dev.orbstack", ".orbstack"}, "docker system prune -a", false},
	{"Docker Desktop", []string{"Library/Containers/com.docker.docker"}, "docker system prune -a; не пользуешься - удалить само приложение", false},
	{"npm", []string{".npm"}, "npm cache clean --force", true},
	{"composer", []string{"Library/Caches/composer", ".composer/cache", ".cache/composer"}, "composer clear-cache", true},
	{"Homebrew", []string{"Library/Caches/Homebrew", ".cache/Homebrew"}, "brew cleanup --prune=all", true},
	{"go: модули", []string{"go/pkg/mod"}, "go clean -modcache", true},
	{"go: сборка", []string{"Library/Caches/go-build", ".cache/go-build"}, "go clean -cache", true},
	{"pip", []string{"Library/Caches/pip", ".cache/pip"}, "pip cache purge", true},
	{"bun", []string{".bun/install/cache"}, "bun pm cache rm", true},
	{"pnpm", []string{"Library/pnpm/store", "Library/Caches/pnpm", ".local/share/pnpm/store"}, "pnpm store prune", true},
	{"yarn", []string{"Library/Caches/Yarn", ".cache/yarn"}, "yarn cache clean", true},
	{"JetBrains: кеши IDE", []string{"Library/Caches/JetBrains", ".cache/JetBrains"}, "папки прошлых версий IDE удаляются целиком, текущую не трогать", false},
	{"Xcode DerivedData", []string{"Library/Developer/Xcode/DerivedData"}, "rm -rf ~/Library/Developer/Xcode/DerivedData", true},
	{"VS Code: кеш", []string{"Library/Application Support/Code/Cache", "Library/Application Support/Code/CachedData", ".config/Code/Cache", ".config/Code/CachedData"}, "удалить при закрытом редакторе", false},
	{"podman", []string{".local/share/containers"}, "podman system prune -a", false},
	{"docker", []string{"/var/lib/docker"}, "docker system prune -a", false},
	{"журнал systemd", []string{"/var/log/journal"}, "journalctl --vacuum-size=500M", false},
	{"пакеты apt", []string{"/var/cache/apt"}, "apt-get clean", false},
	{"модели Hugging Face", []string{".cache/huggingface"}, "hf cache delete - выбрать ненужные модели", true},
	{"uv", []string{".cache/uv"}, "uv cache clean", true},
	{"браузеры puppeteer", []string{".cache/puppeteer"}, "удалить папку, скачаются снова при надобности", true},
	{"браузеры chrome-devtools-mcp", []string{".cache/chrome-devtools-mcp"}, "удалить папку, скачаются снова при надобности", true},
	{"Корзина", []string{".Trash", ".local/share/Trash"}, "очистить Корзину", false},
	{"Загрузки", []string{"Downloads"}, "", false},
}

// Caches: папка общих кешей; из неё вычитается то, что показано отдельной строкой.
const Caches = "Library/Caches"

// Parents: общие папки кешей. Из них вычитается то, что показано отдельными строками, остаток - "прочее".
var Parents = []struct{ Path, Name, Clean string }{
	{Caches, "~/Library/Caches: прочее", "приложения пересоздадут сами; чистить при закрытых приложениях"},
	{DotCache, "~/.cache: прочее", ""},
}

// DotCache: кеши консольных программ и моделей.
const DotCache = ".cache"

// Place: место, где на машине разработчика копится пересоздаваемое.
type Place struct {
	Name  string
	Paths []string // от домашней папки или абсолютные - системные места сервера
	Clean string   // чем чистить; пусто - разбирать руками
	Whole bool     // папку можно убрать целиком: это кэш, он скачается снова
}

// Measured: место с размером в байтах.
type Measured struct {
	Place
	Size int64
}

// Summarize считает "прочее" в Caches без вложенных мест и сортирует по размеру.
// sizes - размер каждого пути из Paths и Caches, от домашней папки.
func SummarizeSystem(places []Place, sizes map[string]int64) []Measured {
	var out []Measured
	nested := map[string]int64{}
	for _, p := range places {
		var size int64
		for _, path := range p.Paths {
			size += sizes[path]
			for _, parent := range Parents {
				if strings.HasPrefix(path, parent.Path+"/") {
					nested[parent.Path] += sizes[path]
				}
			}
		}
		if size > 0 {
			out = append(out, Measured{Place: p, Size: size})
		}
	}
	for _, parent := range Parents {
		if rest := sizes[parent.Path] - nested[parent.Path]; rest > 0 {
			out = append(out, Measured{Place: Place{Name: parent.Name, Paths: []string{parent.Path}, Clean: parent.Clean}, Size: rest})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Size > out[j].Size })
	return out
}

// AllPaths: все пути, которые надо измерить.
func AllPaths(places []Place) []string {
	seen := map[string]bool{Caches: true, DotCache: true}
	out := []string{Caches, DotCache}
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

// ParseDu: вывод du -k -d N -> размер в байтах по пути от home.
func ParseDu(out, home string) map[string]int64 {
	sizes := map[string]int64{}
	for _, line := range strings.Split(out, "\n") {
		kb, path, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSpace(kb), 10, 64)
		if err != nil {
			continue
		}
		if rel, err := filepath.Rel(home, path); err == nil && !strings.HasPrefix(rel, "..") {
			sizes[rel] = n * 1024
		}
	}
	return sizes
}

// Children: прямые подпапки dir из уже измеренного, по убыванию размера.
func Children(sizes map[string]int64, dir string) []Item {
	var out []Item
	for p, s := range sizes {
		if filepath.Dir(p) == dir {
			out = append(out, Item{Path: filepath.Base(p), Size: s})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Size > out[j].Size })
	return out
}

// Under: путь лежит внутри одного из корней - его размер уже есть в обходе корня.
func Under(path string, roots ...string) bool {
	for _, r := range roots {
		if strings.HasPrefix(path, r+"/") {
			return true
		}
	}
	return false
}
