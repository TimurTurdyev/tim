// Package leftover: что осталось в ~/Library от программ, которых уже нет.
// Только решения по именам и путям; обход диска и запуск утилит - в cmd.
package leftover

import (
	"regexp"
	"strings"
)

var teamPrefix = regexp.MustCompile(`^[A-Z0-9]{10}\.`)

var second = map[string]bool{"co": true, "com": true, "org": true, "net": true, "ac": true, "gov": true}

// keep: папки самой macOS и общие, у которых нет своей программы.
var keep = map[string]bool{}

// App: установленная программа.
type App struct{ ID, Name string }

// Index: по чему узнавать установленные программы - их ID, поставщики (com.google) и имена.
type Index struct {
	vendors map[string]bool
	ids     map[string]bool
	names   map[string]bool
}

func NewIndex(apps []App) Index {
	ix := Index{vendors: map[string]bool{}, ids: map[string]bool{}, names: map[string]bool{}}
	for _, a := range apps {
		id := strings.ToLower(a.ID)
		if id != "" {
			ix.ids[id] = true
			ix.vendors[vendor(id)] = true
			// Имя поставщика из ID тоже имя: у Google Chrome папка Application Support/Google.
			if parts := strings.Split(id, "."); len(parts) >= 2 {
				ix.names[simplify(parts[1])] = true
			}
		}
		if a.Name != "" {
			ix.names[simplify(a.Name)] = true
		}
	}
	return ix
}

// BundleID: имя папки или файла в Library как ID программы: без .plist и .savedState, без
// префикса команды разработчика у Group Containers и без group. в начале. Пусто - имя не ID.
func BundleID(name string) string {
	for _, suf := range []string{".plist", ".savedState", ".binarycookies"} {
		name = strings.TrimSuffix(name, suf)
	}
	name = teamPrefix.ReplaceAllString(name, "")
	name = strings.TrimPrefix(name, "group.")
	name = strings.TrimPrefix(name, "groups.")
	parts := strings.Split(name, ".")
	if len(parts) < 3 {
		return ""
	}
	switch strings.ToLower(parts[0]) {
	case "com", "org", "net", "io", "ru", "de", "dev", "app", "ai", "co", "me", "jp", "uk", "fr", "us", "tv", "cc", "sh", "so":
		return strings.ToLower(name)
	}
	return ""
}

// vendor: поставщик по ID - com.google, а у доменов вида uk.co.hellogames - три части.
func vendor(id string) string {
	parts := strings.Split(id, ".")
	if len(parts) < 2 {
		return id
	}
	if len(parts) >= 4 && len(parts[0]) == 2 && second[parts[1]] {
		return strings.Join(parts[:3], ".")
	}
	return parts[0] + "." + parts[1]
}

// Verdict: чьё это и осталось ли от удалённой программы.
type Verdict struct {
	Owner   string // поставщик (com.valvesoftware) или имя папки
	Orphan  bool
	Sure    bool   // по ID - точно; по имени - "похоже"
	Similar string // установлена программа с тем же именем, но другим ID: другая версия или сборка
}

// Judge решает по имени записи в Library. Системное Apple и всё, что узнаётся по установленной
// программе того же поставщика, не трогается: лучше пропустить хвост, чем назвать хвостом нужное.
func (ix Index) Judge(name string) Verdict {
	if id := BundleID(name); id != "" {
		v := vendor(id)
		if v == "com.apple" || ix.ids[id] || ix.vendors[v] {
			return Verdict{Owner: v}
		}
		orig := strings.Split(strings.TrimSuffix(strings.TrimSuffix(name, ".plist"), ".savedState"), ".")
		similar := ""
		if last := orig[len(orig)-1]; ix.names[simplify(last)] {
			similar = last
		}
		return Verdict{Owner: v, Orphan: true, Sure: true, Similar: similar}
	}
	key := simplify(name)
	if key == "" || ix.names[key] || keep[key] {
		return Verdict{Owner: name}
	}
	for n := range ix.names {
		// "Docker Desktop" и Docker, "Code" и Visual Studio Code: имя папки внутри имени программы или наоборот.
		if len(n) >= 4 && len(key) >= 4 && (strings.Contains(n, key) || strings.Contains(key, n)) {
			return Verdict{Owner: name}
		}
	}
	return Verdict{Owner: name, Orphan: true}
}

func simplify(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r >= 'а' && r <= 'я' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func init() {
	for _, n := range []string{"AddressBook", "Animoji", "App Store", "AskPermission", "CallHistoryDB", "CallHistoryTransactions",
		"Caches", "Categories", "CloudDocs", "CrashReporter", "DifferentialPrivacy", "DiskImages", "Dock", "FaceTime",
		"FileProvider", "Knowledge", "MobileSync", "Mobile Documents", "SyncServices", "icdd", "iCloud", "networkserviced",
		"ControlCenter", "contactsd", "familycircled", "default.store", "default.store-shm", "default.store-wal",
		"Logs", "Preferences", "Containers", "LaunchAgents", "Saved Application State", "HTTPStorages", "WebKit",
		"Assistants", "Audio", "ColorPickers", "Compositions", "Favorites", "FontCollections", "Fonts", "Frameworks",
		"Input Methods", "Internet Plug-Ins", "Keyboard Layouts", "Keychains", "PreferencePanes", "Screen Savers",
		"Services", "Sounds", "Spelling", "Widgets", "Accounts", "Metadata", "Group Containers", "Daemon Containers",
		"Developer", "Shortcuts", "Mail", "Messages", "Safari", "Photos", "Music", "TV", "Podcasts", "Weather",
		"Stocks", "News", "Maps", "Reminders", "Calendars", "Notes", "Passes", "Trial", "PersonalizationPortrait",
		"homeenergyd", "identityservicesd", "locationaccessstored", "Biome", "Autosave Information", "Cookies",
		"Printers", "Sharing", "StatusKit", "Suggestions", "UnifiedAssetFramework", "Translation", "Wallet",
		"ScreenRecordings", "Staging", "tipsd", "studentd", "transparencyd", "Group", "Applications"} {
		keep[simplify(n)] = true
	}
}

// AgentTarget: что запускает агент launchd - Program или первый из ProgramArguments.
func AgentTarget(program string, args []string) string {
	if program != "" {
		return program
	}
	if len(args) > 0 {
		return args[0]
	}
	return ""
}
