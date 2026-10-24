package cmd

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/TimurTurdyev/tim/internal/leftover"
	"github.com/TimurTurdyev/tim/internal/procs"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var diskLeft bool

func leftovers() error {
	if tui.Available() && !diskClean {
		return runScreen(leftScreen())
	}
	groups, err := leftover.Gather(ownName, runningProcs())
	if err != nil {
		return err
	}
	var shown []*leftover.Group
	var total, smallSize int64
	small := 0
	for _, g := range groups {
		if !diskAll && g.Size < 50<<20 && len(g.Agents) == 0 && len(g.PIDs) == 0 {
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
	if diskClean {
		return cleanLeftovers(shown)
	}
	ui.Println(ui.Dim("пути от ~/Library") + "\n")
	for _, g := range shown {
		printLeftGroup(g)
	}
	if small > 0 {
		ui.Println(ui.Dim("ещё " + strconv.Itoa(small) + " мелких на " + ui.Bytes(smallSize) + ": tim disk --leftovers --all"))
	}
	ui.Println(ui.Info("всего " + ui.Bytes(total)))
	ui.Println(ui.Dim("  убрать, отметив нужное:   tim disk --leftovers --clean"))
	ui.Println(ui.Dim("  только посмотреть команды: tim disk --leftovers --clean --dry-run"))
	return nil
}

// devFolder: папка с таким именем есть среди проектов в корне разработки (~/Dev/projects/my-app),
// даже если её нет в реестре.
func devFolder(name string) bool {
	for _, pat := range []string{filepath.Join(cfg.Dev, name), filepath.Join(cfg.Dev, "*", name)} {
		if m, _ := filepath.Glob(pat); len(m) > 0 {
			return true
		}
	}
	return false
}

func printLeftGroup(g *leftover.Group) {
	sort.Slice(g.Items, func(i, j int) bool { return g.Items[i].Size > g.Items[j].Size })
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
		if it.Note == "" && it.Size < 1<<20 && !diskAll {
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
		ui.Println("          " + ui.Dim(ui.Pad(ui.Bytes(tinySize), 10)+"ещё "+strconv.Itoa(tiny)+" "+ui.Plural(tiny, "мелочь", "мелочи", "мелочей")+": настройки, журналы, кеши"))
	}
	ui.Println()
}

// cleanLeftovers: в терминале - список с галочками, что убрать, и одно подтверждение; без него -
// вопрос по каждой. Команды печатаются по мере выполнения, с --dry-run - только печатаются.
// "Похоже" заранее не отмечено: узнано по одному имени.
func cleanLeftovers(groups []*leftover.Group) error {
	picked := groups
	if tui.Available() && !runner.DryRun {
		items := make([]tui.Item, len(groups))
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
			items[i] = tui.Item{Title: ui.Pad(ui.Bytes(g.Size), 10) + g.Name(), Hint: hint}
		}
		idx, err := tui.MultiSelect("Что убрать в Корзину?", items, nil)
		if err != nil {
			return err
		}
		picked = nil
		var size int64
		for _, i := range idx {
			picked = append(picked, groups[i])
			size += groups[i].Size
		}
		if len(picked) == 0 {
			ui.Println(ui.Dim("ничего не выбрано"))
			return nil
		}
		ok, err := tui.Confirm("Убрать "+strconv.Itoa(len(picked))+" "+ui.Plural(len(picked), "программу", "программы", "программ")+", "+ui.Bytes(size)+"?", false)
		if err != nil || !ok {
			return err
		}
	}
	var freed int64
	for _, g := range picked {
		ui.Println(ui.Label(ui.Pad(ui.Bytes(g.Size), 10)) + g.Title())
		if !tui.Available() && !runner.DryRun && !runner.ConfirmDefault("убрать "+g.Name()+"?", false) {
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

// ownName: папка с именем проекта из реестра или из корня разработки - свои данные, не хвост.
func ownName(name string) bool {
	l := strings.ToLower(name)
	for _, p := range cfg.Projects {
		if l == strings.ToLower(p.Name) || l == strings.ToLower(filepath.Base(cfg.ProjectDir(&p))) {
			return true
		}
	}
	return devFolder(name)
}

func runningProcs() []procs.Full {
	list, _ := allProcs()
	return list
}
