package motd

import (
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/TimurTurdyev/tim/internal/brand"
	"github.com/TimurTurdyev/tim/internal/bypass"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/par"
	"github.com/TimurTurdyev/tim/internal/sysinfo"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// sections: разделы сводки по порядку. Раздел, не успевший к сроку, пропускается: сводка не должна
// задерживать открытие терминала.
var sections = []section{
	{"system", 800 * time.Millisecond, system},
	{"project", 800 * time.Millisecond, project},
	{"net", 800 * time.Millisecond, network},
	{"ports", 800 * time.Millisecond, ports},
	{"services", 800 * time.Millisecond, services},
	{"repos", 2500 * time.Millisecond, repos},
	{"procs", 800 * time.Millisecond, hogs},
	{"versions", 800 * time.Millisecond, versions},
}

var defaultSections = []string{"system", "project", "net", "ports", "services"}

// artGap: отступ сводки от маскота.
const artGap = "   "

// seenBypass: обход, посчитанный разделом сети, - маскоту не считать его второй раз.
var seenBypass atomic.Pointer[[2]int]

// section: раздел сводки и сколько его ждать.
type section struct {
	name    string
	timeout time.Duration
	collect func(*config.Config) []string
}

// result: строки раздела и успел ли он.
type result struct {
	lines []string
	took  time.Duration
	late  bool
}

// show: выбранные разделы, собранные разом; timing - сколько занял каждый.
func show(cfg *config.Config, want []string, all, timing bool) error {
	chosen, err := choose(cfg, want, all)
	if err != nil {
		return err
	}
	start := time.Now()
	seenBypass.Store(nil)
	results := make([]result, len(sections))
	g := par.NewGroup(len(sections))
	for i, s := range sections {
		if chosen[s.name] {
			g.Go(func() { results[i] = collect(cfg, s) })
		}
	}
	g.Wait()
	var out []string
	for i, s := range sections {
		if !chosen[s.name] {
			continue
		}
		out = append(out, results[i].lines...)
		if timing {
			note := fmt.Sprintf("%s %d мс", s.name, results[i].took.Milliseconds())
			if results[i].late {
				note += ", не успел"
			}
			out = append(out, ui.Dim("  "+note))
		}
	}
	if timing {
		out = append(out, ui.Dim(fmt.Sprintf("всего %d мс", time.Since(start).Milliseconds())))
	}
	// Рядом с одной-двумя строками шесть строк картинки лишние.
	if len(out) >= 3 {
		out = withArt(out, health(cfg).Mood(time.Now()))
	}
	for _, l := range out {
		ui.Println(l)
	}
	return nil
}

// health: признаки беды для маскота - диск, память, страж; обход - если его посчитал раздел сети.
func health(cfg *config.Config) Health {
	var h Health
	if d, ok := sysinfo.DataDisk(); ok {
		h.DiskPct = d.Percent()
	}
	if used, total := sysinfo.Memory(); total > 0 {
		h.MemPct = int(used * 100 / total)
	}
	if len(cfg.Hosts) > 0 {
		h.GuardMissing = !bypass.GuardAlive()
	}
	if b := seenBypass.Load(); b != nil {
		h.BypassOK, h.BypassAll = b[0], b[1]
	}
	return h
}

// withArt: маскот слева от сводки, если терминал цветной и вмещает обе колонки; иначе строки как есть.
func withArt(lines []string, mood brand.Mood) []string {
	on, depth := ui.Colors()
	if !on {
		return lines
	}
	art := brand.Mascot(mood, depth)
	widest := 0
	for _, l := range lines {
		widest = max(widest, ui.Width(l))
	}
	if len(art) == 0 || ui.Cols() < brand.MascotWidth+len(artGap)+widest {
		return lines
	}
	// Верхняя строка картинки пустая - место для "z" во сне; у короткой сводки без неё куб вровень с текстом.
	if len(lines) < len(art) {
		art = art[1:]
	}
	out := make([]string, max(len(art), len(lines)))
	for i := range out {
		left := strings.Repeat(" ", brand.MascotWidth)
		if i < len(art) {
			left = art[i]
		}
		out[i] = left
		if i < len(lines) {
			out[i] += artGap + lines[i]
		}
	}
	return out
}

// choose: разделы из аргументов, а без них - ключ motd конфига или разделы по умолчанию.
func choose(cfg *config.Config, want []string, all bool) (map[string]bool, error) {
	switch {
	case all:
		want = names()
	case len(want) == 0 && len(cfg.Motd) > 0:
		want = cfg.Motd
	case len(want) == 0:
		want = defaultSections
	}
	known := names()
	chosen := map[string]bool{}
	for _, w := range want {
		if !slices.Contains(known, w) {
			return nil, ui.Errorf("нет раздела %s: %s", w, strings.Join(known, ", "))
		}
		chosen[w] = true
	}
	return chosen, nil
}

// collect: раздел, но не дольше его срока; опоздавший досчитается в фоне и пропадёт.
func collect(cfg *config.Config, s section) result {
	t0 := time.Now()
	done := make(chan []string, 1)
	go func() { done <- s.collect(cfg) }()
	select {
	case lines := <-done:
		return result{lines: lines, took: time.Since(t0)}
	case <-time.After(s.timeout):
		return result{took: s.timeout, late: true}
	}
}

func names() []string {
	out := make([]string, len(sections))
	for i, s := range sections {
		out[i] = s.name
	}
	return out
}
