package disk

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/leftover"
	"github.com/TimurTurdyev/tim/internal/procs"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// leftovers: хвосты удалённых программ - экраном в терминале, иначе списком; с clean - уборка.
func leftovers(env *app.Env, o options) error {
	if app.Interactive() && !o.clean {
		return env.Open(LeftoversScreen(env.Cfg))
	}
	groups, err := gatherLeftovers(env.Cfg)
	if err != nil {
		return err
	}
	var shown []*leftover.Group
	var total, smallSize int64
	small := 0
	for _, g := range groups {
		if !o.all && minor(g) {
			small++
			smallSize += g.Size
			continue
		}
		shown = append(shown, g)
		total += g.Size
	}
	if len(shown) == 0 {
		ui.Println(ui.Dim("хвостов удалённых программ не нашёл"))
		return nil
	}
	if o.clean {
		return cleanLeftovers(shown)
	}
	ui.Println(ui.Dim("пути от ~/Library") + "\n")
	for _, g := range shown {
		printLeftGroup(g, o.all)
	}
	if small > 0 {
		ui.Println(ui.Dim("ещё " + strconv.Itoa(small) + " мелких на " + ui.Bytes(smallSize) + ": tim disk --leftovers --all"))
	}
	ui.Println(ui.Info("всего " + ui.Bytes(total)))
	ui.Println(ui.Dim("  убрать, отметив нужное:   tim disk --leftovers --clean"))
	ui.Println(ui.Dim("  только посмотреть команды: tim disk --leftovers --clean --dry-run"))
	return nil
}

func gatherLeftovers(cfg *config.Config) ([]*leftover.Group, error) {
	running, _ := procs.All()
	return leftover.Gather(ownNames(cfg), running)
}

// minor: мелочь без агентов и процессов - по умолчанию не показывается.
func minor(g *leftover.Group) bool {
	return g.Size < 50<<20 && len(g.Agents) == 0 && len(g.PIDs) == 0
}

// ownNames: имена проектов реестра и папок в корне разработки (~/Dev/projects/my-app), в нижнем
// регистре - папка с таким именем в ~/Library своя, а не хвост.
func ownNames(cfg *config.Config) map[string]bool {
	own := map[string]bool{}
	for i := range cfg.Projects {
		own[strings.ToLower(cfg.Projects[i].Name)] = true
		own[strings.ToLower(filepath.Base(cfg.ProjectDir(&cfg.Projects[i])))] = true
	}
	for _, pat := range []string{filepath.Join(cfg.Dev, "*"), filepath.Join(cfg.Dev, "*", "*")} {
		dirs, _ := filepath.Glob(pat)
		for _, d := range dirs {
			if st, err := os.Stat(d); err == nil && st.IsDir() {
				own[strings.ToLower(filepath.Base(d))] = true
			}
		}
	}
	return own
}

func printLeftGroup(g *leftover.Group, all bool) {
	sort.SliceStable(g.Items, func(i, j int) bool { return g.Items[i].Size > g.Items[j].Size })
	title := g.Title()
	if !g.Sure {
		title += ui.Dim("  похоже, от удалённой программы - проверь")
	}
	ui.Println(ui.Label(ui.Pad(ui.Bytes(g.Size), 10)) + title)
	if g.Similar != "" {
		ui.Println("          " + ui.Warn("установлен другой "+g.Similar+": это данные другой версии, проверь, что они не нужны"))
	}
	tiny, tinySize := 0, int64(0)
	for _, it := range g.Items {
		// Мелочь вроде настроек и куки - одной строкой: смотреть на неё незачем, убирается вместе со всем.
		if it.Note == "" && it.Size < 1<<20 && !all {
			tiny++
			tinySize += it.Size
			continue
		}
		line := "          "
		if it.Path != "" {
			line += ui.Pad(ui.Bytes(it.Size), 10) + leftover.LibPath(it.Path)
		}
		if it.Note != "" {
			line += "  " + ui.Warn(it.Note)
		}
		ui.Println(line)
	}
	if tiny > 0 {
		ui.Println("          " + ui.Dim(ui.Pad(ui.Bytes(tinySize), 10)+"ещё "+ui.Count(tiny, "мелочь", "мелочи", "мелочей")+": настройки, журналы, кеши"))
	}
	ui.Println()
}

// cleanLeftovers: в терминале - список с галочками, что убрать, и одно подтверждение; без него -
// вопрос по каждой. Команды печатаются по мере выполнения, с --dry-run - только печатаются.
func cleanLeftovers(groups []*leftover.Group) error {
	picked := groups
	interactive := app.Interactive()
	if interactive && !runner.DryRun {
		var err error
		if picked, err = pickLeftovers(groups); err != nil || len(picked) == 0 {
			return err
		}
	}
	var freed int64
	for _, g := range picked {
		ui.Println(ui.Label(ui.Pad(ui.Bytes(g.Size), 10)) + g.Title())
		if !interactive && !runner.DryRun && !runner.ConfirmDefault("убрать "+g.Name()+"?", false) {
			ui.Println()
			continue
		}
		if leftover.Remove(g) {
			freed += g.Size
		}
		ui.Println()
	}
	switch {
	case freed > 0 && runner.DryRun:
		ui.Println(ui.Info("ушло бы в Корзину "+ui.Bytes(freed)) + ui.Dim("  это --dry-run, ничего не тронуто"))
	case freed > 0:
		ui.Println(ui.Info("в Корзине "+ui.Bytes(freed)) + ui.Dim("  место освободится, когда очистишь Корзину"))
	}
	return nil
}

// pickLeftovers: что убрать, галочками; "похоже" заранее не отмечено - узнано по одному имени.
func pickLeftovers(groups []*leftover.Group) ([]*leftover.Group, error) {
	opts := make([]app.Option, len(groups))
	for i, g := range groups {
		hint := ""
		if !g.Sure {
			hint = "похоже, проверь"
		}
		if hint == "" && len(g.Items) > 0 && g.Items[0].Path != "" {
			hint = leftover.LibPath(g.Items[0].Path)
			if len(g.Items) > 1 {
				hint += " и ещё " + strconv.Itoa(len(g.Items)-1)
			}
		}
		opts[i] = app.Option{Title: ui.Pad(ui.Bytes(g.Size), 10) + g.Name(), Hint: hint}
	}
	idx, err := app.ChooseMany("Что убрать в Корзину?", opts)
	if err != nil {
		return nil, err
	}
	var picked []*leftover.Group
	var size int64
	for _, i := range idx {
		picked = append(picked, groups[i])
		size += groups[i].Size
	}
	if len(picked) == 0 {
		ui.Println(ui.Dim("ничего не выбрано"))
		return nil, nil
	}
	ok, err := app.Ask("Убрать "+ui.Count(len(picked), "программу", "программы", "программ")+", "+ui.Bytes(size)+"?", false)
	if err != nil || !ok {
		return nil, err
	}
	return picked, nil
}
