package disk

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/TimurTurdyev/tim/internal/diskuse"
	"github.com/TimurTurdyev/tim/internal/par"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/shellq"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// printSystem: куда делось место кроме проектов - кеши программ по именам. Только показ и команды,
// ничего не удаляет.
func printSystem() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	sizes := measureSystem(home)
	var total int64
	for _, m := range diskuse.SummarizeSystem(diskuse.SystemPlaces, sizes) {
		if m.Size < 50<<20 {
			continue
		}
		total += m.Size
		hint := []string{placePath(m.Place, sizes)}
		if m.Clean != "" {
			hint = append(hint, engineHint(m.Name, m.Clean))
		}
		ui.Println(ui.Label(ui.Pad(ui.Bytes(m.Size), 10)) + ui.Pad(m.Name, 28) + ui.Dim(strings.Join(hint, "  \u2022  ")))
		printPlaceDetails(m.Paths[0], sizes, home)
	}
	ui.Println(ui.Info("всего "+ui.Bytes(total)) + ui.Dim("  места крупнее 50 MB; tim ничего не удаляет"))
	return nil
}

// measureSystem: общие кеши - одним du на корень, из него же берутся размеры вложенных мест
// и топ прочего; отдельно меряется только то, что лежит вне них.
func measureSystem(home string) map[string]int64 {
	roots := map[string]string{diskuse.Caches: "2", diskuse.DotCache: "1"}
	sizes := map[string]int64{}
	var mu sync.Mutex
	g := par.NewGroup(6)
	measure := func(rel string, args ...string) {
		full := rel
		if !filepath.IsAbs(rel) {
			full = filepath.Join(home, rel)
		}
		if _, err := os.Stat(full); err != nil {
			return
		}
		g.Go(func() {
			// du без прав на часть подпапок пишет ошибки, но итог печатает - его и берём.
			out, _ := runner.OutputWithin(0, "du", append(args, full)...)
			got := diskuse.ParseDu(out, home)
			if filepath.IsAbs(rel) {
				got = map[string]int64{rel: diskuse.ParseDu(out, "/")[strings.TrimPrefix(rel, "/")]}
			}
			mu.Lock()
			maps.Copy(sizes, got)
			mu.Unlock()
		})
	}
	for rel, depth := range roots {
		measure(rel, "-k", "-d", depth)
	}
	for _, rel := range diskuse.AllPaths(diskuse.SystemPlaces) {
		if _, root := roots[rel]; !root && !diskuse.Under(rel, diskuse.Caches, diskuse.DotCache) {
			measure(rel, "-sk")
		}
	}
	g.Wait()
	return sizes
}

// printPlaceDetails: что внутри общих кешей, моделей и кешей IDE.
func printPlaceDetails(place string, sizes map[string]int64, home string) {
	switch place {
	case diskuse.DotCache, diskuse.Caches:
		for _, sub := range topCaches(sizes, place, true, 3) {
			ui.Println(ui.Dim(subRow(sub.Size, sub.Path, tilde(filepath.Join(place, sub.Path)))))
		}
	case ".cache/huggingface":
		printModels(filepath.Join(home, ".cache", "huggingface", "hub"))
	case diskuse.Caches + "/JetBrains":
		printIDECaches(place, sizes)
	}
}

func printIDECaches(place string, sizes map[string]int64) {
	subs := topCaches(sizes, place, false, 20)
	names := make([]string, len(subs))
	for i, sub := range subs {
		names[i] = sub.Path
	}
	old := diskuse.OldIDEVersions(names)
	for _, sub := range subs {
		if sub.Size < 50<<20 {
			continue
		}
		rel := filepath.Join(place, sub.Path)
		if !old[sub.Path] {
			ui.Println(ui.Dim(subRow(sub.Size, sub.Path, tilde(rel))))
			continue
		}
		// Готовая команда: trash кладёт в Корзину, а не стирает - передумал, вернул.
		// Тильда остаётся снаружи кавычек, иначе shell её не раскроет.
		ui.Println(ui.Dim("          "+ui.Pad(ui.Bytes(sub.Size), 10)+ui.Pad(sub.Path, 24)) +
			ui.Warn("старая версия") + ui.Dim("  trash ~/"+shellq.Join([]string{rel})))
	}
}

// printModels: модели Hugging Face по размеру и с тем, когда их последний раз запускали:
// без этого непонятно, какую из моделей по 8 ГБ можно убрать.
func printModels(hub string) {
	models := diskuse.HFModels(hub)
	g := par.NewGroup(par.Limit)
	for i := range models {
		g.Go(func() {
			models[i].Size = diskuse.ModelSize(models[i].Path)
			models[i].LastUsed = diskuse.LastUsed(models[i].Path)
		})
	}
	g.Wait()
	diskuse.SortModels(models)
	for _, m := range models {
		if m.Size < 50<<20 {
			continue
		}
		ui.Println(ui.Dim("          "+ui.Pad(ui.Bytes(m.Size), 10)+ui.Pad(m.Name, 44)) + ui.Pad(usedText(m.LastUsed), 24) +
			ui.Dim(tilde(filepath.Join(".cache", "huggingface", "hub", filepath.Base(m.Path)))))
	}
}

func usedText(last time.Time) string {
	if last.IsZero() {
		return "не запускали"
	}
	days := int(time.Since(last).Hours() / 24)
	switch {
	case days == 0:
		return "запускали сегодня"
	case days < 30:
		return "запускали " + strconv.Itoa(days) + " дн назад"
	}
	return ui.Warn("не запускали " + strconv.Itoa(days) + " дн")
}

// topCaches: самые большие подпапки общего кеша, кроме тех, что уже показаны отдельной строкой.
func topCaches(sizes map[string]int64, rel string, skipShown bool, n int) []diskuse.Item {
	var items []diskuse.Item
	for _, it := range diskuse.Children(sizes, rel) {
		if skipShown && shownPlace(rel+"/"+it.Path) {
			continue
		}
		items = append(items, it)
	}
	return items[:min(n, len(items))]
}

func shownPlace(rel string) bool {
	for _, p := range diskuse.SystemPlaces {
		if slices.Contains(p.Paths, rel) {
			return true
		}
	}
	return false
}

// placePath: где лежит место; у мест из нескольких папок - самая большая из них.
func placePath(p diskuse.Place, sizes map[string]int64) string {
	best := p.Paths[0]
	for _, path := range p.Paths {
		if sizes[path] > sizes[best] {
			best = path
		}
	}
	return tilde(best)
}

func subRow(size int64, name, path string) string {
	return "          " + ui.Pad(ui.Bytes(size), 10) + ui.Pad(name, 24) + path
}

// tilde: путь от домашней папки так, как его набирают в терминале.
func tilde(rel string) string {
	if filepath.IsAbs(rel) {
		return rel
	}
	return "~/" + rel
}

// engineHint: docker system prune чистит только через работающий движок - у выключенного
// OrbStack нет сокета, и команда падает с "no such file or directory".
func engineHint(name, clean string) string {
	if name != "OrbStack" || !runner.Has("orb") {
		return clean
	}
	out, _ := runner.Output("orb", "status")
	if strings.TrimSpace(out) == "Running" {
		return clean
	}
	return "OrbStack выключен: orb start, " + clean + ", orb stop"
}
