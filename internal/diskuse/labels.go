package diskuse

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

var rules = []rule{
	{"*/steamapps/common", Label{Name: "игры Steam", Hint: "удалить в Steam: Библиотека - игра - Управление - Удалить", Stop: true}, true},
	{"Library/Containers/com.utmapp.UTM/Data/Documents", Label{Name: "виртуалки UTM", Hint: "ненужные удалить в самом UTM", Stop: true}, true},
	{"Library/Containers/com.isaacmarovitz.Whisky/Bottles", Label{Name: "Whisky: Windows-программы и игры", Hint: "ненужные бутылки удалить в Whisky", Stop: true}, true},
	{"Parallels", Label{Name: "виртуалки Parallels", Hint: "удалить в Parallels Desktop: Центр управления", Stop: true}, true},
	{"Library/Application Support/MobileSync/Backup", Label{Name: "резервные копии iPhone и iPad", Hint: "Finder - устройство - Управлять резервными копиями", Stop: true, Kind: Data}, false},
	{"Library/Developer/CoreSimulator", Label{Name: "симуляторы Xcode", Hint: "xcrun simctl delete unavailable", Stop: true}, false},
	{"Library/Developer/Xcode/iOS DeviceSupport", Label{Name: "Xcode: символы устройств", Hint: "старые версии iOS можно удалить, скачаются при подключении", Stop: true}, false},
	{".android/avd", Label{Name: "эмуляторы Android", Hint: "ненужные удалить в Android Studio - Device Manager", Stop: true}, true},
	{"Library/Android/sdk", Label{Name: "Android SDK", Hint: "лишние образы и платформы - Android Studio - SDK Manager", Stop: true}, false},
	{"Pictures/Photos Library.photoslibrary", Label{Name: "медиатека Фото", Hint: "Фото - Настройки - iCloud - Оптимизировать хранилище", Stop: true, Kind: Data}, false},
	{"Library/Mail", Label{Name: "почта", Hint: "большие вложения удаляются в самой Почте", Stop: true, Kind: Data}, false},
	{"Library/Messages", Label{Name: "вложения сообщений", Hint: "Об этом Mac - Хранилище - Сообщения", Stop: true, Kind: Data}, false},
	{".ollama/models", Label{Name: "модели Ollama", Hint: "ollama list, затем ollama rm <модель>", Stop: true}, false},
	{".lmstudio/models", Label{Name: "модели LM Studio", Hint: "удалить в LM Studio - My Models", Stop: true}, false},
	{"/Applications", Label{Name: "программы", Hint: "ненужные - в Корзину из Программ", Stop: true, Kind: Apps}, true},
	{"Applications", Label{Name: "программы в домашней папке", Hint: "ненужные - в Корзину", Stop: true, Kind: Apps}, true},
	{"Downloads", Label{Name: "Загрузки", Hint: "разобрать руками", Stop: true}, false},
	{".Trash", Label{Name: "Корзина", Hint: "очистить Корзину", Stop: true}, false},
	{".local/share/Trash", Label{Name: "Корзина", Hint: "очистить Корзину", Stop: true}, false},
	{"/opt/homebrew", Label{Name: "Homebrew: программы", Hint: "brew autoremove; brew cleanup; ненужное - brew uninstall", Stop: true, Kind: Apps}, false},
	{"/var/lib/docker", Label{Name: "docker", Hint: "docker system prune -a", Stop: true}, false},
	{"/var/log", Label{Name: "журналы системы", Hint: "journalctl --vacuum-size=500M; старые логи в /var/log", Stop: true, Kind: System}, false},
}

var deps = map[string]bool{"node_modules": true, "vendor": true, ".venv": true, "venv": true, "target": true, ".next": true, ".nuxt": true}

var plistName = regexp.MustCompile(`<key>name</key>\s*<string>([^<]+)</string>`)

var (
	indexMu sync.Mutex
	indexes = map[string]*labelIndex{}
)

// Label: что это за место и как его освободить. Group - папка, все места внутри которой
// показываются одной строкой с разбивкой (игры Steam, виртуалки).
type Label struct {
	Name, Hint, Group string
	Stop              bool // внутрь не спускаться: это одно целое
	Whole             bool // можно убрать целиком, пересоздастся само
	Kind              Kind
}

// Kind: раздел обзора - что можно освободить без потерь, программы и то, что решать человеку.
type Kind int

const (
	Free Kind = iota
	Apps
	Data
	System
)

type rule struct {
	path  string // от домашней папки или абсолютный; */ в начале - в любом месте пути
	label Label
	group bool // путь rule - группа, а места внутри - её строки
}

// LabelOf: подпись места. ok=false - ничего про место не известно, показывается как есть.
func LabelOf(path, home string) (Label, string, bool) {
	ix := indexFor(home)
	if i, base, ok := ix.rule(path); ok {
		r := rules[i]
		if !r.group {
			return r.label, "", true
		}
		// Внутрь группы спускаемся до её строк: игра, виртуалка, бутылка - одно целое.
		l := r.label
		l.Stop = path != base
		return l, base, true
	}
	name := filepath.Base(path)
	switch {
	case deps[name]:
		return Label{Name: "зависимости " + name, Hint: "пересоздаются установкой; заброшенные - tim disk --clean", Stop: true, Whole: true}, "", true
	case name == "storage" && exists(filepath.Join(filepath.Dir(path), "artisan")):
		return Label{Name: "файлы Laravel", Hint: "загрузки и логи: перед удалением проверь, что они есть на сервере", Stop: true, Kind: Data}, "", true
	case name == "backups" || name == "backup" || strings.Contains(path, "/backups/"):
		return Label{Name: "бэкапы", Hint: "старые копии - на внешний диск или в облако", Stop: name == "backups" || name == "backup", Kind: Data}, "", true
	case strings.HasSuffix(name, ".app"):
		return Label{Name: "приложение " + strings.TrimSuffix(name, ".app"), Hint: "не нужно - в Корзину из Программ", Stop: true, Kind: Apps}, "", true
	case name == ".git":
		return Label{Name: "история git", Hint: "git gc --prune=now; большие файлы в истории - git filter-repo", Stop: true, Kind: Data}, "", true
	}
	if i, _, ok := lookup(ix.places, path); ok {
		p := SystemPlaces[i]
		return Label{Name: p.Name, Hint: p.Clean, Stop: true, Whole: p.Whole}, "", true
	}
	return Label{}, "", false
}

// labelIndex: пути правил, развёрнутые от домашней папки один раз, - LabelOf зовётся на каждую
// строку обзора и обозревателя.
type labelIndex struct {
	rules  map[string]int // абсолютный путь -> номер в rules
	wild   []int          // номера правил вида */путь, по возрастанию
	places map[string]int // абсолютный путь -> номер в SystemPlaces
}

func indexFor(home string) *labelIndex {
	indexMu.Lock()
	defer indexMu.Unlock()
	if ix, ok := indexes[home]; ok {
		return ix
	}
	ix := &labelIndex{rules: map[string]int{}, places: map[string]int{}}
	abs := func(p string) string {
		if filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(home, p)
	}
	for i, r := range rules {
		if strings.HasPrefix(r.path, "*/") {
			ix.wild = append(ix.wild, i)
		} else if _, dup := ix.rules[abs(r.path)]; !dup {
			ix.rules[abs(r.path)] = i
		}
	}
	for i, p := range SystemPlaces {
		for _, rel := range p.Paths {
			if _, dup := ix.places[abs(rel)]; !dup {
				ix.places[abs(rel)] = i
			}
		}
	}
	indexes[home] = ix
	return ix
}

// rule: первое по списку правило, под которое попадает path, и папка, от которой оно действует.
func (ix *labelIndex) rule(path string) (int, string, bool) {
	i, base, ok := lookup(ix.rules, path)
	for _, w := range ix.wild {
		if ok && w > i {
			break
		}
		needle := rules[w].path[1:]
		j := strings.Index(path, needle)
		if j < 0 {
			continue
		}
		if b := path[:j+len(needle)]; path == b || strings.HasPrefix(path, b+"/") {
			return w, b, true
		}
	}
	return i, base, ok
}

// lookup: среди path и его предков - путь с наименьшим номером, как при проходе списка по порядку.
func lookup(m map[string]int, path string) (int, string, bool) {
	best, base := -1, ""
	for d := path; ; d = filepath.Dir(d) {
		if i, ok := m[d]; ok && (best < 0 || i < best) {
			best, base = i, d
		}
		if filepath.Dir(d) == d {
			break
		}
	}
	return best, base, best >= 0
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// ItemName: человеческое имя строки внутри группы - у бутылок Whisky это не UUID, а имя из
// Metadata.plist, у виртуалок без расширения.
func ItemName(path string) string {
	base := filepath.Base(path)
	if data, err := os.ReadFile(filepath.Join(path, "Metadata.plist")); err == nil {
		if m := plistName.FindSubmatch(data); m != nil {
			return string(m[1])
		}
	}
	for _, ext := range []string{".utm", ".avd", ".pvm", ".app"} {
		base = strings.TrimSuffix(base, ext)
	}
	return base
}
