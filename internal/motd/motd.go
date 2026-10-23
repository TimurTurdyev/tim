// Package motd: сводка при открытии терминала и tim status - лок-ап с маскотом и карточка машины. Те же
// строки показывает главный экран.
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
	"github.com/TimurTurdyev/tim/internal/gitx"
	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/machine"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/par"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/run"
	"github.com/TimurTurdyev/tim/internal/sysinfo"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// sections: разделы карточки по порядку. Раздел, не успевший к сроку, пропускается: сводка не должна
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

// defaultSections: проект текущей папки уже в третьей строке лок-апа, службы не меняются неделями -
// они в --full.
var defaultSections = []string{"system", "net", "ports"}

// Куда ведёт строка карточки на главном экране.
const (
	ToProcs Dest = iota + 1
	ToDisk
	ToNet
	ToRunning
)

// labelW: подпись карточки - 10 колонок и 2 пробела, значение с той же колонки, что текст лок-апа.
const labelW = 12

// Dest: экран, куда ведёт строка карточки.
type Dest int

// Line: строка карточки. Пустая подпись - продолжение строки выше.
type Line struct {
	Label, Value string
	Dest         Dest
}

// Text: строка для терминала, с отступом подписи.
func (l Line) Text() string { return ui.Label(ui.Pad(l.Label, labelW)) + l.Value }

// section: раздел сводки и сколько его ждать.
type section struct {
	name    string
	timeout time.Duration
	collect func(*config.Config) []Line
}

// result: строки раздела и успел ли он.
type result struct {
	lines []Line
	took  time.Duration
	late  bool
}

// show: лок-ап, карточка по выбранным разделам, беды под ней; timing - сколько занял каждый раздел.
func show(cfg *config.Config, build string, want []string, all, full, timing bool) error {
	if full && len(want) == 0 && !all {
		want = append(slices.Clone(defaultSections), "services")
	}
	chosen, err := choose(cfg, want, all)
	if err != nil {
		return err
	}
	start := time.Now()
	var head [][]string
	var rows []Line
	var h health.Machine
	var results []result
	par.All(
		func() { head = Lockup(cfg, build, true) },
		func() { rows, h, results = collectAll(cfg, chosen) },
	)
	_, depth := ui.Colors()
	look := brand.Look{Mood: h.Mood(time.Now()), Light: ui.Light()}
	out := ui.Header(head, ui.HeaderOpts{Look: look, Indent: 1, Width: ui.Cols(), Sprite: depth != brand.Basic})
	if full {
		out = append(out, "")
	}
	for _, r := range rows {
		out = append(out, "  "+r.Text())
	}
	var warns []string
	for _, w := range h.Warnings() {
		warns = append(warns, "  "+ui.Warn("\u25b2 "+w.Full()))
	}
	warns = append(warns, machine.HogLines("  "+ui.Warn("\u25b2 "))...)
	if len(warns) > 0 {
		out = append(append(out, ""), warns...)
	}
	if timing {
		out = append(out, "", "  "+ui.Dim(timingNote(chosen, results, time.Since(start))))
	}
	for _, l := range out {
		ui.Println(l)
	}
	// Пустая строка после сводки: иначе приглашение оболочки прилипает к ней.
	ui.Println()
	return nil
}

func timingNote(chosen map[string]bool, results []result, total time.Duration) string {
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
	return strings.Join(notes, ", ") + fmt.Sprintf("; всего %d мс", total.Milliseconds())
}

// Rows: строки карточки машины по разделам (nil - из конфига или по умолчанию) и признаки беды -
// то же, что в сводке, для главного экрана.
func Rows(cfg *config.Config, want []string) ([]Line, health.Machine) {
	chosen, err := choose(cfg, want, false)
	if err != nil {
		return nil, health.Machine{}
	}
	rows, h, _ := collectAll(cfg, chosen)
	return rows, h
}

// collectAll: разделы параллельно, каждый в свой срок; беды считаются тут же.
func collectAll(cfg *config.Config, chosen map[string]bool) ([]Line, health.Machine, []result) {
	results := make([]result, len(sections))
	var h health.Machine
	g := par.NewGroup(len(sections) + 1)
	g.Go(func() { h = Troubles(cfg) })
	for i, s := range sections {
		if chosen[s.name] {
			g.Go(func() { results[i] = collect(cfg, s) })
		}
	}
	g.Wait()
	var rows []Line
	for i, s := range sections {
		if chosen[s.name] {
			rows = append(rows, results[i].lines...)
		}
	}
	return rows, h, results
}

// Lockup: три строки лок-апа - имя и сборка; машина; где ты. withDate - сводка печатается один раз,
// и время в ней верно; на экране часы врали бы через минуту.
func Lockup(cfg *config.Config, build string, withDate bool) [][]string {
	var who, where []string
	par.All(
		func() { who = machineLine(withDate) },
		func() { where = placeLine(cfg) },
	)
	return [][]string{{ui.Strong(brand.Name) + "  " + ui.Dim(build)}, who, where}
}

// machineLine: пользователь и машина, система, дата, сколько работает, батарея.
func machineLine(withDate bool) []string {
	var osName string
	var boot time.Time
	var booted, battOK bool
	var batt sysinfo.Battery
	par.All(
		func() { osName = sysinfo.OS() },
		func() { boot, booted = sysinfo.Boot() },
		func() { batt, battOK = sysinfo.BatteryNow() },
	)
	user := os.Getenv("USER")
	if u, err := osuser.Current(); err == nil {
		user = u.Username
	}
	host, _ := os.Hostname()
	host, _, _ = strings.Cut(host, ".")
	segs := []string{ui.Dim(user + "@" + host), ui.Dim(osName)}
	if withDate {
		segs = append(segs, ui.Dim(ui.DateShort(time.Now())))
	}
	if booted {
		segs = append(segs, ui.Dim("работает "+ui.Short(time.Since(boot))))
	}
	if battOK {
		segs = append(segs, batteryText(batt))
	}
	return segs
}

func batteryText(b sysinfo.Battery) string {
	text := fmt.Sprintf("батарея %d%%", b.Percent)
	if note := b.Note(); note != "" {
		text += ", " + note
	}
	if b.Low() {
		return ui.Warn(text)
	}
	return ui.Dim(text)
}

// placeLine: папка, откуда запущен tim, и - если она в реестре - проект, стек и git.
func placeLine(cfg *config.Config) []string {
	cwd, _ := os.Getwd()
	segs := []string{ui.Dim(paths.Show(cwd))}
	p, ok := cfg.ProjectHere(cwd)
	if !ok {
		return segs
	}
	dir := cfg.ProjectDir(p)
	segs = append(segs, ui.Dim(p.Name))
	if st := run.Stack(dir); st != "" {
		segs = append(segs, ui.Dim(st))
	}
	if g, ok := gitx.Status(dir); ok {
		segs = append(segs, ui.Dim(g.Branch)+gitDelta(g))
	}
	return segs
}

// gitDelta: что не так с веткой, через пробел после её имени.
func gitDelta(g gitx.State) string {
	if d := g.Delta(); d != "" {
		return " " + d
	}
	return ""
}

// Troubles: признаки беды для маскота и предупреждений - память, диск, страж, обход. Обход - по
// последней проверке стража: резолвить сотни целей при каждом старте терминала слишком долго.
func Troubles(cfg *config.Config) health.Machine {
	var h health.Machine
	par.All(
		func() {
			if d, ok := sysinfo.DataDisk(); ok {
				h.DiskPct, h.DiskFree = d.Percent(), int64(d.Avail)
			}
		},
		func() {
			h.MemFree, _ = sysinfo.MemFree()
			if sw, ok := machine.SwapNow(); ok {
				h.SwapUsed, h.SwapTotal = sw.Used, sw.Total
			}
			if h.MemoryShort() {
				h.MemHolder = machine.MemHolder()
			}
		},
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
	done := make(chan []Line, 1)
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
