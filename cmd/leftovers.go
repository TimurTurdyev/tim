package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/TimurTurdyev/tim/internal/leftover"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var diskLeft bool

// installedApps: программы из /Applications, ~/Applications и системные, с ID из Info.plist.
func installedApps() []leftover.App {
	var bundles []string
	for _, dir := range []string{"/Applications", "/Applications/Utilities", "/System/Applications", "/System/Applications/Utilities",
		filepath.Join(paths.Home(), "Applications")} {
		found, _ := filepath.Glob(filepath.Join(dir, "*.app"))
		nested, _ := filepath.Glob(filepath.Join(dir, "*", "*.app"))
		bundles = append(append(bundles, found...), nested...)
	}
	apps := make([]leftover.App, len(bundles))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, b := range bundles {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			id, _ := runner.Output("plutil", "-extract", "CFBundleIdentifier", "raw", "-o", "-", filepath.Join(b, "Contents", "Info.plist"))
			apps[i] = leftover.App{ID: strings.TrimSpace(id), Name: strings.TrimSuffix(filepath.Base(b), ".app")}
		})
	}
	wg.Wait()
	return apps
}

// libraryPlaces: где программы оставляют своё в ~/Library.
var libraryPlaces = []string{"Application Support", "Caches", "Containers", "Group Containers", "Preferences",
	"Saved Application State", "HTTPStorages", "WebKit", "Logs", "Cookies"}

type leftItem struct {
	path string
	size int64
	note string
}

type leftGroup struct {
	owner   string
	sure    bool
	size    int64
	items   []leftItem
	agents  []string // метки агентов launchd, которые надо выгрузить
	pids    []string
	similar string
	app     string // имя удалённой программы, если известно по пути её файла
}

func leftovers() error {
	if runtime.GOOS != "darwin" {
		return ui.Errorf("хвосты удалённых программ tim пока ищет только на macOS")
	}
	home := paths.Home()
	lib := filepath.Join(home, "Library")
	ix := leftover.NewIndex(installedApps())
	projects := map[string]bool{}
	for _, p := range cfg.Projects {
		projects[strings.ToLower(p.Name)] = true
		projects[strings.ToLower(filepath.Base(cfg.ProjectDir(&p)))] = true
	}
	groups := map[string]*leftGroup{}
	add := func(v leftover.Verdict, it leftItem) *leftGroup {
		g := groups[v.Owner]
		if g == nil {
			g = &leftGroup{owner: v.Owner}
			groups[v.Owner] = g
		}
		g.sure = g.sure || v.Sure
		g.items = append(g.items, it)
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
			if !v.Sure && (projects[strings.ToLower(e.Name())] || devFolder(e.Name())) {
				continue
			}
			// По одному имени, без ID, верим только папкам программ: в Caches и Group Containers
			// такие имена у консольных утилит (go-build, playwright) и у групп с чужими командами.
			if !v.Orphan || (!v.Sure && place != "Application Support") {
				continue
			}
			g := add(v, leftItem{path: filepath.Join(lib, place, e.Name())})
			if v.Similar != "" {
				g.similar = v.Similar
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
			target := leftover.AgentTarget(a.Program, a.ProgramArguments)
			if !filepath.IsAbs(target) {
				continue
			}
			if _, err := os.Stat(target); err == nil {
				continue
			}
			v := ix.Judge(a.Label)
			v.Orphan, v.Sure = true, true
			g := add(v, leftItem{path: f, note: "автозапуск без программы"})
			g.app = appOf(target)
			switch dir {
			case filepath.Join(lib, "LaunchAgents"), "/Library/LaunchAgents":
				g.agents = append(g.agents, "gui/"+strconv.Itoa(os.Getuid())+"/"+a.Label)
			default:
				g.agents = append(g.agents, "system/"+a.Label)
			}
		}
	}
	// Процессы, чей файл уже удалён, - работают с прошлого запуска удалённой программы.
	if list, err := allProcs(); err == nil {
		for _, p := range list {
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
			g := add(leftover.Verdict{Owner: owner, Orphan: true, Sure: true}, leftItem{note: "процесс " + p.PID + " ещё работает, хотя программа удалена"})
			g.pids = append(g.pids, p.PID)
		}
	}
	var all []*leftGroup
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, g := range groups {
		for i := range g.items {
			if g.items[i].path == "" {
				continue
			}
			wg.Go(func() {
				sem <- struct{}{}
				defer func() { <-sem }()
				out, _ := runner.Output("du", "-sk", g.items[i].path)
				kb, _ := strconv.ParseInt(strings.Fields(out + " 0")[0], 10, 64)
				g.items[i].size = kb * 1024
			})
		}
		all = append(all, g)
	}
	wg.Wait()
	var shown []*leftGroup
	var total, smallSize int64
	small := 0
	for _, g := range all {
		for _, it := range g.items {
			g.size += it.size
		}
		// По одному имени без ID - только крупное: мелких папок от утилит без .app слишком много.
		switch {
		case !g.sure && g.size < 1<<30:
		case !diskAll && g.size < 50<<20 && len(g.agents) == 0 && len(g.pids) == 0:
			small++
			smallSize += g.size
		default:
			shown = append(shown, g)
			total += g.size
		}
	}
	if len(shown) == 0 {
		ui.Println(ui.Dim("хвостов удалённых программ не нашёл"))
		return nil
	}
	sort.Slice(shown, func(i, j int) bool { return shown[i].size > shown[j].size })
	if diskClean {
		return cleanLeftovers(shown)
	}
	ui.Println(ui.Dim("пути от ~/Library") + "\n")
	for _, g := range shown {
		printLeftGroup(g)
	}
	if small > 0 {
		ui.Println(ui.Dim("ещё " + strconv.Itoa(small) + " мелких на " + fmtBytes(smallSize) + ": tim disk --leftovers --all"))
	}
	ui.Println(ui.Info("всего " + fmtBytes(total)))
	ui.Println(ui.Dim("  убрать, отметив нужное:   tim disk --leftovers --clean"))
	ui.Println(ui.Dim("  только посмотреть команды: tim disk --leftovers --clean --dry-run"))
	return nil
}

// groupTitle: имя программы по ID её файлов (Whisky, а не com.isaacmarovitz) и поставщик рядом.
func groupTitle(g *leftGroup) string {
	if g.app != "" {
		return g.app + ui.Dim("  "+g.owner)
	}
	// Имя - по самому большому файлу: у Docker это com.docker.docker, а не служебный com.docker.install.
	sort.Slice(g.items, func(i, j int) bool { return g.items[i].size > g.items[j].size })
	for _, it := range g.items {
		name := strings.TrimSuffix(strings.TrimSuffix(filepath.Base(it.path), ".plist"), ".savedState")
		if leftover.BundleID(name) != "" {
			parts := strings.Split(name, ".")
			return parts[len(parts)-1] + ui.Dim("  "+g.owner)
		}
	}
	return g.owner
}

// devFolder: папка с таким именем есть среди проектов в корне разработки (~/Dev/projects/simple-talk),
// даже если её нет в реестре.
func devFolder(name string) bool {
	for _, pat := range []string{filepath.Join(cfg.Dev, name), filepath.Join(cfg.Dev, "*", name)} {
		if m, _ := filepath.Glob(pat); len(m) > 0 {
			return true
		}
	}
	return false
}

// appOf: имя программы по пути внутри её .app.
func appOf(p string) string {
	if i := strings.Index(p, ".app/"); i >= 0 {
		return filepath.Base(p[:i])
	}
	return ""
}

// libPath: путь короче - от ~/Library, раз все хвосты там; остальное как есть.
func libPath(p string) string {
	lib := filepath.Join(paths.Home(), "Library") + "/"
	if strings.HasPrefix(p, lib) {
		return strings.TrimPrefix(p, lib)
	}
	return p
}

func printLeftGroup(g *leftGroup) {
	sort.Slice(g.items, func(i, j int) bool { return g.items[i].size > g.items[j].size })
	title := groupTitle(g)
	if !g.sure {
		title += ui.Dim("  похоже, от удалённой программы - проверь")
	}
	ui.Println(ui.Label(ui.Pad(fmtBytes(g.size), 10)) + title)
	if g.similar != "" {
		ui.Println("          " + ui.Warn("установлен другой "+g.similar+": это данные другой версии, проверь, что они не нужны"))
	}
	tiny, tinySize := 0, int64(0)
	for _, it := range g.items {
		// Мелочь вроде настроек и куки - одной строкой: смотреть на неё незачем, убирается вместе со всем.
		if it.note == "" && it.size < 1<<20 && !diskAll {
			tiny++
			tinySize += it.size
			continue
		}
		line := "          "
		if it.path != "" {
			line += ui.Pad(fmtBytes(it.size), 10) + libPath(it.path)
		}
		if it.note != "" {
			line += "  " + ui.Warn(it.note)
		}
		ui.Println(line)
	}
	if tiny > 0 {
		ui.Println("          " + ui.Dim(ui.Pad(fmtBytes(tinySize), 10)+"ещё "+strconv.Itoa(tiny)+" "+ui.Plural(tiny, "мелочь", "мелочи", "мелочей")+": настройки, журналы, кеши"))
	}
	ui.Println()
}

func splitOwn(g *leftGroup) (mine, system []string) {
	for _, it := range g.items {
		switch {
		case it.path == "":
		case strings.HasPrefix(it.path, paths.Home()+"/"):
			mine = append(mine, it.path)
		default:
			system = append(system, it.path)
		}
	}
	return mine, system
}

// cleanLeftovers: в терминале - список с галочками, что убрать, и одно подтверждение; без него -
// вопрос по каждой. Команды печатаются по мере выполнения, с --dry-run - только печатаются.
// "Похоже" заранее не отмечено: узнано по одному имени.
func cleanLeftovers(groups []*leftGroup) error {
	picked := groups
	if tui.Available() && !runner.DryRun {
		items := make([]tui.Item, len(groups))
		for i, g := range groups {
			hint := ""
			if !g.sure {
				hint = "похоже, проверь"
			}
			if hint == "" && len(g.items) > 0 && g.items[0].path != "" {
				hint = libPath(g.items[0].path)
				if len(g.items) > 1 {
					hint += " и ещё " + strconv.Itoa(len(g.items)-1)
				}
			}
			items[i] = tui.Item{Title: ui.Pad(fmtBytes(g.size), 10) + groupName(g), Hint: hint}
		}
		idx, err := tui.MultiSelect("Что убрать в Корзину?", items, nil)
		if err != nil {
			return err
		}
		picked = nil
		var size int64
		for _, i := range idx {
			picked = append(picked, groups[i])
			size += groups[i].size
		}
		if len(picked) == 0 {
			ui.Println(ui.Dim("ничего не выбрано"))
			return nil
		}
		ok, err := tui.Confirm("Убрать "+strconv.Itoa(len(picked))+" "+ui.Plural(len(picked), "программу", "программы", "программ")+", "+fmtBytes(size)+"?", false)
		if err != nil || !ok {
			return err
		}
	}
	var freed int64
	for _, g := range picked {
		ui.Println(ui.Label(ui.Pad(fmtBytes(g.size), 10)) + groupTitle(g))
		if !tui.Available() && !runner.DryRun && !runner.ConfirmDefault("убрать "+groupName(g)+"?", false) {
			ui.Println()
			continue
		}
		if removeGroup(g) {
			freed += g.size
		}
		ui.Println()
	}
	switch {
	case freed > 0 && runner.DryRun:
		ui.Println(ui.Info("ушло бы в Корзину "+fmtBytes(freed)) + ui.Dim("  это --dry-run, ничего не тронуто"))
	case freed > 0:
		ui.Println(ui.Info("в Корзине "+fmtBytes(freed)) + ui.Dim("  место освободится, когда очистишь Корзину"))
	}
	return nil
}

func groupName(g *leftGroup) string { return strings.Split(groupTitle(g), "  ")[0] }

// removeGroup: агенты выгрузить, процессы остановить, файлы в Корзину; системные - через sudo.
func removeGroup(g *leftGroup) bool {
	sort.Slice(g.items, func(i, j int) bool { return g.items[i].size > g.items[j].size })
	for _, a := range g.agents {
		if strings.HasPrefix(a, "system/") {
			_ = runner.Sudo("launchctl", "bootout", a)
		} else {
			_ = runner.Run("launchctl", "bootout", a)
		}
	}
	if len(g.pids) > 0 {
		_ = runner.Run("kill", g.pids...)
	}
	mine, system := splitOwn(g)
	ok := true
	for _, p := range mine {
		if err := moveToTrash(p); err != nil {
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
