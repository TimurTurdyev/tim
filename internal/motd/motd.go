// Package motd: сводка при открытии терминала и tim status - карточка машины с маскотом. Те же строки
// показывает главный экран.
package motd

import (
	"fmt"
	"os"
	osuser "os/user"
	"slices"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/brand"
	"github.com/TimurTurdyev/tim/internal/bypass"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/netinfo"
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

// show: шапка, карточка по выбранным разделам, предупреждения и маскот; timing - сколько занял каждый.
func show(cfg *config.Config, want []string, all, timing bool) error {
	chosen, err := choose(cfg, want, all)
	if err != nil {
		return err
	}
	start := time.Now()
	var head string
	var rows []string
	var h health.Machine
	var results []result
	par.All(
		func() { head = header() },
		func() { rows, h, results = collectAll(cfg, chosen) },
	)
	out := append([]string{head}, rows...)
	for _, w := range h.Warnings() {
		out = append(out, ui.Warn("\u25b2 "+w.Text))
	}
	if timing {
		var notes []string
		for i, s := range sections {
			if !chosen[s.name] {
				continue
			}
			note := fmt.Sprintf("%s %d мс", s.name, results[i].took.Milliseconds())
			if results[i].late {
				note += " (не успел)"
			}
			notes = append(notes, note)
		}
		out = append(out, ui.Dim(strings.Join(notes, ", ")+fmt.Sprintf("; всего %d мс", time.Since(start).Milliseconds())))
	}
	// Рядом с одной-двумя строками шесть строк картинки лишние.
	if len(out) >= 3 {
		out = withArt(out, h.Mood(time.Now()))
	}
	for _, l := range out {
		ui.Println(l)
	}
	// Пустая строка после карточки: иначе приглашение оболочки прилипает к маскоту.
	ui.Println()
	return nil
}

// Rows: строки карточки машины по разделам (nil - из конфига или по умолчанию) и признаки беды -
// то же, что в сводке, для главного экрана.
func Rows(cfg *config.Config, want []string) ([]string, health.Machine) {
	chosen, err := choose(cfg, want, false)
	if err != nil {
		return nil, health.Machine{}
	}
	rows, h, _ := collectAll(cfg, chosen)
	return rows, h
}

// collectAll: разделы параллельно, каждый в свой срок; беды считаются тут же.
func collectAll(cfg *config.Config, chosen map[string]bool) ([]string, health.Machine, []result) {
	results := make([]result, len(sections))
	var h health.Machine
	g := par.NewGroup(len(sections) + 1)
	g.Go(func() { h = troubles(cfg) })
	for i, s := range sections {
		if chosen[s.name] {
			g.Go(func() { results[i] = collect(cfg, s) })
		}
	}
	g.Wait()
	var rows []string
	for i, s := range sections {
		if chosen[s.name] {
			rows = append(rows, results[i].lines...)
		}
	}
	return rows, h, results
}

// header: кто и где - пользователь, машина, система, дата и сколько работает.
func header() string {
	var osName string
	var boot time.Time
	var booted bool
	par.All(
		func() { osName = sysinfo.OS() },
		func() { boot, booted = sysinfo.Boot() },
	)
	user := os.Getenv("USER")
	if u, err := osuser.Current(); err == nil {
		user = u.Username
	}
	host, _ := os.Hostname()
	host, _, _ = strings.Cut(host, ".")
	line := ui.Label(user+"@"+host) + "  " + osName + "  " + ui.DateShort(time.Now())
	if booted {
		line += ui.Dim("  работает " + ui.Duration(time.Since(boot)))
	}
	return line
}

// troubles: признаки беды для маскота и предупреждений - диск, память, страж, обход. Обход - по
// последней проверке стража: резолвить сотни целей при каждом старте терминала слишком долго.
func troubles(cfg *config.Config) health.Machine {
	var h health.Machine
	par.All(
		func() {
			if d, ok := sysinfo.DataDisk(); ok {
				h.DiskPct, h.DiskFree = d.Percent(), int64(d.Avail)
			}
		},
		func() { h.MemFree, _ = sysinfo.MemFree() },
		func() {
			if len(cfg.Hosts) == 0 {
				return
			}
			h.GuardMissing = !bypass.GuardAlive()
			if netinfo.IsTunnelIface(netinfo.InternetIface()) {
				h.BypassOK, h.BypassAll, _ = bypass.QuickCount()
			}
		},
	)
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
