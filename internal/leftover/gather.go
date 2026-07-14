package leftover

import (
	"cmp"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/TimurTurdyev/tim/internal/diskuse"
	"github.com/TimurTurdyev/tim/internal/par"
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
	return par.Map(bundles, par.Limit, func(b string) App {
		id, _ := runner.Output("plutil", "-extract", "CFBundleIdentifier", "raw", "-o", "-", filepath.Join(b, "Contents", "Info.plist"))
		return App{ID: strings.TrimSpace(id), Name: strings.TrimSuffix(filepath.Base(b), ".app")}
	})
}

// Gather: всё, что осталось от удалённых программ, по убыванию размера; мелочь по имени отброшена.
// own - свои имена в нижнем регистре (проекты, папки разработки): папка с таким именем не хвост.
func Gather(own map[string]bool, running []procs.Full) ([]*Group, error) {
	if runtime.GOOS != "darwin" {
		return nil, ui.Errorf("хвосты удалённых программ tim пока ищет только на macOS")
	}
	home := paths.Home()
	g := &gathering{lib: filepath.Join(home, "Library"), index: NewIndex(Installed()), groups: map[string]*Group{}}
	g.libraryDirs(own)
	g.orphanAgents()
	g.deadProcesses(running, home)
	return g.measured(), nil
}

// gathering: хвосты, сложенные по владельцу, пока идёт поиск.
type gathering struct {
	lib    string
	index  Index
	groups map[string]*Group
}

func (g *gathering) add(v Verdict, it Item) *Group {
	grp := g.groups[v.Owner]
	if grp == nil {
		grp = &Group{Owner: v.Owner}
		g.groups[v.Owner] = grp
	}
	grp.Sure = grp.Sure || v.Sure
	grp.Items = append(grp.Items, it)
	return grp
}

// libraryDirs: папки в ~/Library, чья программа не установлена.
func (g *gathering) libraryDirs(own map[string]bool) {
	for _, place := range libraryPlaces {
		entries, _ := os.ReadDir(filepath.Join(g.lib, place))
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".") {
				continue
			}
			v := g.index.Judge(e.Name())
			// Папка с именем проекта из реестра - его же данные: свои программы в разработке не хвост.
			if !v.Sure && own[strings.ToLower(e.Name())] {
				continue
			}
			// По одному имени, без ID, верим только папкам программ: в Caches и Group Containers
			// такие имена у консольных утилит (go-build, playwright) и у групп с чужими командами.
			if !v.Orphan || (!v.Sure && place != "Application Support") {
				continue
			}
			grp := g.add(v, Item{Path: filepath.Join(g.lib, place, e.Name())})
			if v.Similar != "" {
				grp.Similar = v.Similar
			}
		}
	}
}

// orphanAgents: агенты автозапуска, чья программа удалена, - хвост наверняка: они срабатывают
// при каждом входе.
func (g *gathering) orphanAgents() {
	userAgents := filepath.Join(g.lib, "LaunchAgents")
	for _, dir := range []string{userAgents, "/Library/LaunchAgents", "/Library/LaunchDaemons"} {
		files, _ := filepath.Glob(filepath.Join(dir, "*.plist"))
		for _, f := range files {
			label, target, ok := readAgent(f)
			if !ok || !filepath.IsAbs(target) {
				continue
			}
			if _, err := os.Stat(target); err == nil {
				continue
			}
			v := g.index.Judge(label)
			v.Orphan, v.Sure = true, true
			grp := g.add(v, Item{Path: f, Note: "автозапуск без программы"})
			grp.App = appOf(target)
			if dir == userAgents || dir == "/Library/LaunchAgents" {
				grp.Agents = append(grp.Agents, "gui/"+strconv.Itoa(os.Getuid())+"/"+label)
			} else {
				grp.Agents = append(grp.Agents, "system/"+label)
			}
		}
	}
}

// readAgent: метка агента и программа, которую он запускает.
func readAgent(plist string) (label, target string, ok bool) {
	out, err := runner.Output("plutil", "-convert", "json", "-o", "-", plist)
	if err != nil {
		return "", "", false
	}
	var a struct {
		Label            string   `json:"Label"`
		Program          string   `json:"Program"`
		ProgramArguments []string `json:"ProgramArguments"`
	}
	if json.Unmarshal([]byte(out), &a) != nil {
		return "", "", false
	}
	return a.Label, AgentTarget(a.Program, a.ProgramArguments), true
}

// deadProcesses: процессы, чей файл уже удалён, - работают с прошлого запуска удалённой программы.
func (g *gathering) deadProcesses(running []procs.Full, home string) {
	support := filepath.Join(g.lib, "Application Support")
	for _, p := range running {
		if !filepath.IsAbs(p.Comm) || !strings.HasPrefix(p.Comm, home) {
			continue
		}
		if _, err := os.Stat(p.Comm); err == nil {
			continue
		}
		// Чей процесс - по папке программы, из которой он запущен: Application Support/Steam/... - Steam.
		owner := filepath.Base(p.Comm)
		if r, err := filepath.Rel(support, p.Comm); err == nil && !strings.HasPrefix(r, "..") {
			owner = strings.Split(r, "/")[0]
		}
		grp := g.add(Verdict{Owner: owner, Orphan: true, Sure: true}, Item{Note: "процесс " + p.PID + " ещё работает, хотя программа удалена"})
		grp.PIDs = append(grp.PIDs, p.PID)
	}
}

// measured: размеры хвостов параллельно; найденное только по имени остаётся, если оно крупное.
func (g *gathering) measured() []*Group {
	sizes := par.NewGroup(par.Limit)
	for _, grp := range g.groups {
		for i := range grp.Items {
			if grp.Items[i].Path != "" {
				sizes.Go(func() { grp.Items[i].Size = diskuse.SizeOf(grp.Items[i].Path) })
			}
		}
	}
	sizes.Wait()
	var out []*Group
	for _, grp := range g.groups {
		for _, it := range grp.Items {
			grp.Size += it.Size
		}
		// По одному имени без ID - только крупное: мелких папок от утилит без .app слишком много.
		if !grp.Sure && grp.Size < 1<<30 {
			continue
		}
		out = append(out, grp)
	}
	slices.SortFunc(out, func(a, b *Group) int {
		return cmp.Or(cmp.Compare(b.Size, a.Size), cmp.Compare(a.Owner, b.Owner))
	})
	return out
}

// Title: имя программы по ID её файлов (Whisky, а не com.isaacmarovitz) и поставщик рядом.
func (g *Group) Title() string {
	if g.App != "" {
		return g.App + ui.Dim("  "+g.Owner)
	}
	// Имя - по самому большому файлу: у Docker это com.docker.docker, а не служебный com.docker.install.
	sortItems(g.Items)
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
	sortItems(g.Items)
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

// sortItems: крупные первыми, при равном размере по пути.
func sortItems(items []Item) {
	slices.SortStableFunc(items, func(a, b Item) int { return cmp.Or(cmp.Compare(b.Size, a.Size), cmp.Compare(a.Path, b.Path)) })
}
