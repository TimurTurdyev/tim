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
	"github.com/TimurTurdyev/tim/internal/shellq"
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
}

func leftovers() error {
	if runtime.GOOS != "darwin" {
		return ui.Errorf("хвосты удалённых программ tim пока ищет только на macOS")
	}
	home := paths.Home()
	lib := filepath.Join(home, "Library")
	ix := leftover.NewIndex(installedApps())
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
			g := add(v, leftItem{path: f, note: "автозапуск, программы " + showPath(target) + " уже нет"})
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
			g := add(leftover.Verdict{Owner: owner, Orphan: true, Sure: true}, leftItem{note: "процесс " + p.PID + " работает, а его файла " + showPath(p.Comm) + " уже нет"})
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
	for _, g := range shown {
		printLeftGroup(g)
	}
	if small > 0 {
		ui.Println(ui.Dim("ещё " + strconv.Itoa(small) + " мелких на " + fmtBytes(smallSize) + ": tim disk --leftovers --all"))
	}
	ui.Println(ui.Info("всего "+fmtBytes(total)) + ui.Dim("  команды кладут в Корзину; \"похоже\" - узнано по имени, проверь глазами"))
	return nil
}

func printLeftGroup(g *leftGroup) {
	sort.Slice(g.items, func(i, j int) bool { return g.items[i].size > g.items[j].size })
	title := g.owner
	for _, it := range g.items {
		if id := leftover.BundleID(filepath.Base(it.path)); id != "" {
			parts := strings.Split(filepath.Base(it.path), ".")
			title = strings.TrimSuffix(strings.TrimSuffix(parts[len(parts)-1], "plist"), "savedState") + ui.Dim("  "+g.owner)
			break
		}
	}
	if !g.sure {
		title += ui.Dim("  похоже, от удалённой программы")
	}
	if g.similar != "" {
		title += ui.Warn("  установлен другой " + g.similar + ": это данные другой версии, проверь, что они не нужны")
	}
	ui.Println(ui.Label(ui.Pad(fmtBytes(g.size), 10)) + title)
	sort.Slice(g.items, func(i, j int) bool { return g.items[i].size > g.items[j].size })
	var trash []string
	for _, it := range g.items {
		line := "          "
		if it.path != "" {
			line += ui.Pad(fmtBytes(it.size), 10) + showPath(it.path)
			trash = append(trash, it.path)
		}
		if it.note != "" {
			line += "  " + ui.Warn(it.note)
		}
		ui.Println(line)
	}
	for _, a := range g.agents {
		cmd := "launchctl bootout " + a
		if strings.HasPrefix(a, "system/") {
			cmd = "sudo " + cmd
		}
		ui.Println("          " + ui.Dim(cmd))
	}
	if len(g.pids) > 0 {
		ui.Println("          " + ui.Dim("kill "+strings.Join(g.pids, " ")))
	}
	var mine, system []string
	for _, p := range trash {
		if strings.HasPrefix(p, paths.Home()+"/") {
			mine = append(mine, p)
		} else {
			system = append(system, p)
		}
	}
	if len(mine) > 0 {
		ui.Println("          " + ui.Dim("trash "+shellq.Join(mine)))
	}
	// Системные файлы принадлежат root: trash под sudo унёс бы их в Корзину root, а не в твою.
	for _, p := range system {
		ui.Println("          " + ui.Dim("sudo mv "+shellq.Join([]string{p})+" ~/.Trash/"))
	}
	ui.Println()
}
