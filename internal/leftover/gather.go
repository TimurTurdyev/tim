package leftover

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/procs"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/trash"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Поиск и уборка хвостов: папки в ~/Library, агенты автозапуска и процессы удалённых программ.

// libraryPlaces: где программы оставляют своё в ~/Library.
var libraryPlaces = []string{"Application Support", "Caches", "Containers", "Group Containers", "Preferences",
	"Saved Application State", "HTTPStorages", "WebKit", "Logs", "Cookies"}

// Item: файл или папка хвоста; Path пуст у процесса.
type Item struct {
	Path string
	Size int64
	Note string
}

// Group: всё, что осталось от одной программы.
type Group struct {
	Owner   string
	Sure    bool
	Size    int64
	Items   []Item
	Agents  []string // метки агентов launchd, которые надо выгрузить
	PIDs    []string
	Similar string
	App     string // имя удалённой программы, если известно по пути её файла
}

// Installed: программы из /Applications, ~/Applications и системные, с ID из Info.plist.
func Installed() []App {
	var bundles []string
	for _, dir := range []string{"/Applications", "/Applications/Utilities", "/System/Applications", "/System/Applications/Utilities",
		filepath.Join(paths.Home(), "Applications")} {
		found, _ := filepath.Glob(filepath.Join(dir, "*.app"))
		nested, _ := filepath.Glob(filepath.Join(dir, "*", "*.app"))
		bundles = append(append(bundles, found...), nested...)
	}
	apps := make([]App, len(bundles))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, b := range bundles {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			id, _ := runner.Output("plutil", "-extract", "CFBundleIdentifier", "raw", "-o", "-", filepath.Join(b, "Contents", "Info.plist"))
			apps[i] = App{ID: strings.TrimSpace(id), Name: strings.TrimSuffix(filepath.Base(b), ".app")}
		})
	}
	wg.Wait()
	return apps
}

// Gather: всё, что осталось от удалённых программ, по убыванию размера; мелочь по имени отброшена.
func Gather(own func(name string) bool, running []procs.Full) ([]*Group, error) {
	if runtime.GOOS != "darwin" {
		return nil, ui.Errorf("хвосты удалённых программ tim пока ищет только на macOS")
	}
	home := paths.Home()
	lib := filepath.Join(home, "Library")
	ix := NewIndex(Installed())
	groups := map[string]*Group{}
	add := func(v Verdict, it Item) *Group {
		g := groups[v.Owner]
		if g == nil {
			g = &Group{Owner: v.Owner}
			groups[v.Owner] = g
		}
		g.Sure = g.Sure || v.Sure
		g.Items = append(g.Items, it)
		return g
	}
	for _, place := range libraryPlaces {
		entries, _ := os.ReadDir(filepath.Join(lib, place))
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".") {
				continue
			}
			v := ix.Judge(e.Name())
			// Папка с именем проекта из реестра - его же данные: свои программы в разработке не хвост.
			if !v.Sure && own(e.Name()) {
				continue
			}
			// По одному имени, без ID, верим только папкам программ: в Caches и Group Containers
			// такие имена у консольных утилит (go-build, playwright) и у групп с чужими командами.
			if !v.Orphan || (!v.Sure && place != "Application Support") {
				continue
			}
			g := add(v, Item{Path: filepath.Join(lib, place, e.Name())})
			if v.Similar != "" {
				g.Similar = v.Similar
			}
		}
	}
	// Агенты автозапуска, чья программа удалена, - хвост наверняка: они срабатывают при каждом входе.
	for _, dir := range []string{filepath.Join(lib, "LaunchAgents"), "/Library/LaunchAgents", "/Library/LaunchDaemons"} {
		files, _ := filepath.Glob(filepath.Join(dir, "*.plist"))
		for _, f := range files {
			out, err := runner.Output("plutil", "-convert", "json", "-o", "-", f)
			if err != nil {
				continue
			}
			var a struct {
				Label            string   `json:"Label"`
				Program          string   `json:"Program"`
				ProgramArguments []string `json:"ProgramArguments"`
			}
			if json.Unmarshal([]byte(out), &a) != nil {
				continue
			}
			target := AgentTarget(a.Program, a.ProgramArguments)
			if !filepath.IsAbs(target) {
				continue
			}
			if _, err := os.Stat(target); err == nil {
				continue
			}
			v := ix.Judge(a.Label)
			v.Orphan, v.Sure = true, true
			g := add(v, Item{Path: f, Note: "автозапуск без программы"})
			g.App = appOf(target)
			switch dir {
			case filepath.Join(lib, "LaunchAgents"), "/Library/LaunchAgents":
				g.Agents = append(g.Agents, "gui/"+strconv.Itoa(os.Getuid())+"/"+a.Label)
			default:
				g.Agents = append(g.Agents, "system/"+a.Label)
			}
		}
	}
	// Процессы, чей файл уже удалён, - работают с прошлого запуска удалённой программы.
	{
		for _, p := range running {
			if !filepath.IsAbs(p.Comm) || !strings.HasPrefix(p.Comm, home) {
				continue
			}
			if _, err := os.Stat(p.Comm); err == nil {
				continue
			}
			// Чей процесс - по папке программы, из которой он запущен: Application Support/Steam/... - Steam.
			owner := filepath.Base(p.Comm)
			if r, err := filepath.Rel(filepath.Join(lib, "Application Support"), p.Comm); err == nil && !strings.HasPrefix(r, "..") {
				owner = strings.Split(r, "/")[0]
			}
			g := add(Verdict{Owner: owner, Orphan: true, Sure: true}, Item{Note: "процесс " + p.PID + " ещё работает, хотя программа удалена"})
			g.PIDs = append(g.PIDs, p.PID)
		}
	}
	var all []*Group
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, g := range groups {
		for i := range g.Items {
			if g.Items[i].Path == "" {
				continue
			}
			wg.Go(func() {
				sem <- struct{}{}
				defer func() { <-sem }()
				out, _ := runner.OutputWithin(0, "du", "-sk", g.Items[i].Path)
				kb, _ := strconv.ParseInt(strings.Fields(out + " 0")[0], 10, 64)
				g.Items[i].Size = kb * 1024
			})
		}
		all = append(all, g)
	}
	wg.Wait()
	var out []*Group
	for _, g := range all {
		for _, it := range g.Items {
			g.Size += it.Size
		}
		// По одному имени без ID - только крупное: мелких папок от утилит без .app слишком много.
		if !g.Sure && g.Size < 1<<30 {
			continue
		}
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Size > out[j].Size })
	return out, nil
}

// title: имя программы по ID её файлов (Whisky, а не com.isaacmarovitz) и поставщик рядом.
// Title: имя программы по ID её файлов (Whisky, а не com.isaacmarovitz) и поставщик рядом.
func (g *Group) Title() string {
	if g.App != "" {
		return g.App + ui.Dim("  "+g.Owner)
	}
	// Имя - по самому большому файлу: у Docker это com.docker.docker, а не служебный com.docker.install.
	sort.Slice(g.Items, func(i, j int) bool { return g.Items[i].Size > g.Items[j].Size })
	for _, it := range g.Items {
		name := strings.TrimSuffix(strings.TrimSuffix(filepath.Base(it.Path), ".plist"), ".savedState")
		if BundleID(name) != "" {
			parts := strings.Split(name, ".")
			return parts[len(parts)-1] + ui.Dim("  "+g.Owner)
		}
	}
	return g.Owner
}

// appOf: имя программы по пути внутри её .app.
func appOf(p string) string {
	if i := strings.Index(p, ".app/"); i >= 0 {
		return filepath.Base(p[:i])
	}
	return ""
}

// LibPath: путь короче - от ~/Library, раз все хвосты там; остальное как есть.
func LibPath(p string) string {
	lib := filepath.Join(paths.Home(), "Library") + "/"
	if strings.HasPrefix(p, lib) {
		return strings.TrimPrefix(p, lib)
	}
	return p
}

// SplitOwn: что лежит в домашней папке, а что в системной (туда нужен sudo).
func (g *Group) SplitOwn() (mine, system []string) {
	for _, it := range g.Items {
		switch {
		case it.Path == "":
		case strings.HasPrefix(it.Path, paths.Home()+"/"):
			mine = append(mine, it.Path)
		default:
			system = append(system, it.Path)
		}
	}
	return mine, system
}

// Name: имя без поставщика.
func (g *Group) Name() string { return strings.Split(g.Title(), "  ")[0] }

// Remove: агенты выгрузить, процессы остановить, файлы в Корзину; системные - через sudo.
func Remove(g *Group) bool {
	sort.Slice(g.Items, func(i, j int) bool { return g.Items[i].Size > g.Items[j].Size })
	for _, a := range g.Agents {
		if strings.HasPrefix(a, "system/") {
			_ = runner.Sudo("launchctl", "bootout", a)
		} else {
			_ = runner.Run("launchctl", "bootout", a)
		}
	}
	if len(g.PIDs) > 0 {
		_ = runner.Run("kill", g.PIDs...)
	}
	mine, system := g.SplitOwn()
	ok := true
	for _, p := range mine {
		if err := trash.Move(p); err != nil {
			ui.Println(ui.Bad(err.Error()))
			ok = false
		}
	}
	for _, p := range system {
		if err := runner.Sudo("mv", p, filepath.Join(paths.Home(), ".Trash")+"/"); err != nil {
			ui.Println(ui.Bad(p + ": не убрано"))
			ok = false
		}
	}
	return ok
}
