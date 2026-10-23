package diskuse

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Label: что это за место и как его освободить. Group - папка, все места внутри которой
// показываются одной строкой с разбивкой (игры Steam, виртуалки).
type Label struct {
	Name, Hint, Group string
	Stop              bool // внутрь не спускаться: это одно целое
}

type rule struct {
	path  string // от домашней папки или абсолютный; */ в начале - в любом месте пути
	label Label
	group bool // путь rule - группа, а места внутри - её строки
}

var rules = []rule{
	{"*/steamapps/common", Label{Name: "игры Steam", Hint: "удалить в Steam: Библиотека - игра - Управление - Удалить", Stop: true}, true},
	{"Library/Containers/com.utmapp.UTM/Data/Documents", Label{Name: "виртуалки UTM", Hint: "ненужные удалить в самом UTM", Stop: true}, true},
	{"Library/Containers/com.isaacmarovitz.Whisky/Bottles", Label{Name: "Whisky: Windows-программы и игры", Hint: "ненужные бутылки удалить в Whisky", Stop: true}, true},
	{"Parallels", Label{Name: "виртуалки Parallels", Hint: "удалить в Parallels Desktop: Центр управления", Stop: true}, true},
	{"Library/Application Support/MobileSync/Backup", Label{Name: "резервные копии iPhone и iPad", Hint: "Finder - устройство - Управлять резервными копиями", Stop: true}, false},
	{"Library/Developer/CoreSimulator", Label{Name: "симуляторы Xcode", Hint: "xcrun simctl delete unavailable", Stop: true}, false},
	{"Library/Developer/Xcode/iOS DeviceSupport", Label{Name: "Xcode: символы устройств", Hint: "старые версии iOS можно удалить, скачаются при подключении", Stop: true}, false},
	{".android/avd", Label{Name: "эмуляторы Android", Hint: "ненужные удалить в Android Studio - Device Manager", Stop: true}, true},
	{"Library/Android/sdk", Label{Name: "Android SDK", Hint: "лишние образы и платформы - Android Studio - SDK Manager", Stop: true}, false},
	{"Pictures/Photos Library.photoslibrary", Label{Name: "медиатека Фото", Hint: "Фото - Настройки - iCloud - Оптимизировать хранилище", Stop: true}, false},
	{"Library/Mail", Label{Name: "почта", Hint: "большие вложения удаляются в самой Почте", Stop: true}, false},
	{"Library/Messages", Label{Name: "вложения сообщений", Hint: "Об этом Mac - Хранилище - Сообщения", Stop: true}, false},
	{".ollama/models", Label{Name: "модели Ollama", Hint: "ollama list, затем ollama rm <модель>", Stop: true}, false},
	{".lmstudio/models", Label{Name: "модели LM Studio", Hint: "удалить в LM Studio - My Models", Stop: true}, false},
	{"/Applications", Label{Name: "программы", Hint: "ненужные - в Корзину из Программ", Stop: true}, true},
	{"Applications", Label{Name: "программы в домашней папке", Hint: "ненужные - в Корзину", Stop: true}, true},
	{"Downloads", Label{Name: "Загрузки", Hint: "разобрать руками", Stop: true}, false},
	{".Trash", Label{Name: "Корзина", Hint: "очистить Корзину", Stop: true}, false},
	{".local/share/Trash", Label{Name: "Корзина", Hint: "очистить Корзину", Stop: true}, false},
	{"/opt/homebrew", Label{Name: "Homebrew: программы", Hint: "brew autoremove; brew cleanup; ненужное - brew uninstall", Stop: true}, false},
	{"/var/lib/docker", Label{Name: "docker", Hint: "docker system prune -a", Stop: true}, false},
	{"/var/log", Label{Name: "журналы системы", Hint: "journalctl --vacuum-size=500M; старые логи в /var/log", Stop: true}, false},
}

var deps = map[string]bool{"node_modules": true, "vendor": true, ".venv": true, "venv": true, "target": true, ".next": true, ".nuxt": true}

// LabelOf: подпись места. ok=false - ничего про место не известно, показывается как есть.
func LabelOf(path, home string) (Label, string, bool) {
	for _, r := range rules {
		base := r.path
		switch {
		case strings.HasPrefix(base, "*/"):
			needle := base[1:]
			i := strings.Index(path, needle)
			if i < 0 {
				continue
			}
			base = path[:i+len(needle)]
		case !filepath.IsAbs(base):
			base = filepath.Join(home, base)
		}
		if path == base || strings.HasPrefix(path, base+"/") {
			if !r.group {
				return r.label, "", true
			}
			// Внутрь группы спускаемся до её строк: игра, виртуалка, бутылка - одно целое.
			l := r.label
			l.Stop = path != base
			return l, base, true
		}
	}
	name := filepath.Base(path)
	switch {
	case deps[name]:
		return Label{Name: "зависимости " + name, Hint: "пересоздаются установкой; заброшенные - tim disk --clean", Stop: true}, "", true
	case name == "storage" && exists(filepath.Join(filepath.Dir(path), "artisan")):
		return Label{Name: "файлы Laravel (storage)", Hint: "загрузки пользователей и логи: проверь, что они есть на сервере", Stop: true}, "", true
	case strings.HasSuffix(name, ".app"):
		return Label{Name: "приложение " + strings.TrimSuffix(name, ".app"), Hint: "не нужно - в Корзину из Программ", Stop: true}, "", true
	case name == ".git":
		return Label{Name: "история git", Hint: "git gc --prune=now; большие файлы в истории - git filter-repo", Stop: true}, "", true
	}
	for _, p := range SystemPlaces {
		for _, rel := range p.Paths {
			full := rel
			if !filepath.IsAbs(rel) {
				full = filepath.Join(home, rel)
			}
			if path == full || strings.HasPrefix(path, full+"/") {
				return Label{Name: p.Name, Hint: p.Clean, Stop: true}, "", true
			}
		}
	}
	return Label{}, "", false
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

var plistName = regexp.MustCompile(`<key>name</key>\s*<string>([^<]+)</string>`)

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
