package cmd

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/run"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Экраны на доске: VPN и обход, dev-серверы, проверка проектов. Данные - те же, что печатают
// команды, только разложены по строкам и панелям.

func lamp(level health.Level, text string) string {
	switch level {
	case health.Bad:
		return ui.Bad("\u2715 " + text)
	case health.Warn:
		return ui.Warn("\u25b2 " + text)
	case health.Skip:
		return ui.Dim("- " + text)
	}
	return ui.OK("\u25cf " + text)
}

// ---------- VPN и обход ----------

func vpnScreen() tui.Screen {
	probe := false
	return tui.NewBoard(tui.BoardOpts{
		Crumbs:  []string{"Сеть", "VPN и обход"},
		Command: func(*tui.BoardRow) string { return "tim vpn" },
		Loading: "смотрю туннели и маршруты...",
		Load:    func(bool) tui.BoardData { return vpnData(probe) },
		Actions: []tui.BoardAction{
			{Key: "f", Label: "починить", Do: func(*tui.BoardRow) tui.Action {
				return tui.Action{Exit: func() { runAfter([]string{"vpn", "fix"}) }}
			}},
			{Key: "t", Label: "проверить порты", Run: func(*tui.BoardRow) (string, error) {
				probe = true
				return "порт 22 проверен у каждой цели", nil
			}},
			{Key: "d", Label: "убрать цель", Do: func(row *tui.BoardRow) tui.Action {
				if row == nil {
					return tui.Action{}
				}
				return tui.Action{Exit: func() { runAfter([]string{"route", "rm", row.ID}) }}
			}},
			{Key: "s", Label: "страж", Do: func(*tui.BoardRow) tui.Action { return resultAction([]string{"route", "status"}) }},
		},
	})
}

func vpnData(probe bool) tui.BoardData {
	var d tui.BoardData
	phys, gw := physical()
	d.Status = phys.Name + " " + phys.Device + " \u00b7 шлюз " + dashIfEmpty(gw)
	tunnels := netinfo.Tunnels()
	inet := netinfo.InternetIface()
	var vpn string
	switch {
	case netinfo.IsTunnelIface(inet):
		vpn = ui.OK("\u25cf VPN включён ") + ui.Dim(inet+", весь трафик в туннель")
	case len(tunnels) > 0:
		vpn = ui.OK("\u25cf VPN включён ") + ui.Dim("раздельный туннель, интернет через "+dashIfEmpty(inet))
	default:
		vpn = ui.Dim("\u25cb VPN выключен, интернет напрямую через " + dashIfEmpty(inet))
	}
	var guard string
	st, hasSt := guardStatus()
	switch {
	case !guardInstalled():
		guard = ui.Warn("\u25b2 страж не установлен ") + ui.Dim("после смены сети обход сам не починится: sudo tim route install")
	case !guardRunning():
		guard = ui.Bad("\u2715 страж не работает ") + ui.Dim("tim route status")
	case !hasSt:
		guard = ui.OK("\u25cf страж работает ") + ui.Dim("отчёта пока нет")
	case time.Since(st.LastCheck) > time.Minute:
		guard = ui.Warn("\u25b2 страж молчит "+human(time.Since(st.LastCheck))) + ui.Dim("  tim route status")
	default:
		guard = ui.OK("\u25cf страж работает ") + ui.Dim("проверка "+human(time.Since(st.LastCheck))+" назад")
	}
	d.Head = append(d.Head, ui.Pad(vpn, 60)+guard)
	if len(cfg.Hosts) == 0 {
		d.Head = append(d.Head, ui.Dim("целей обхода нет: tim route add <адрес|домен|подсеть|as:N>"))
		d.Empty = "цели обхода появятся здесь"
		return d
	}
	type target struct {
		row  tui.BoardRow
		bad  int
		dest int
	}
	var targets []target
	total, ok := 0, 0
	for _, h := range cfg.Hosts {
		dests := expandAddr(h.Addr)
		t := target{row: tui.BoardRow{ID: h.Addr, Title: h.Addr, Sub: h.Label}, dest: len(dests)}
		if len(dests) == 0 {
			t.bad = 1
			t.row.Pane = []string{ui.Bad("не раскрывается в адреса: домен не резолвится или AS без сетей")}
		}
		for _, dest := range dests {
			total++
			state, bad := routeState(dest, phys, gw)
			if !bad && bypassed(dest, phys, gw) {
				ok++
			}
			if bad {
				t.bad++
			}
			line := ui.Pad(dest, 20) + state
			if probe && !strings.Contains(dest, "/") {
				if portOpen(dest, "22") {
					line += ui.Dim("  порт 22 отвечает")
				} else {
					line += ui.Bad("  порт 22 молчит")
				}
			}
			if len(t.row.Pane) < 8 {
				t.row.Pane = append(t.row.Pane, line)
			}
		}
		if rest := len(dests) - 8; rest > 0 {
			t.row.Pane = append(t.row.Pane, ui.Dim(fmt.Sprintf("и ещё %d", rest)))
		}
		if t.bad > 0 {
			t.row.Pane = append(t.row.Pane, ui.Dim("\u2192 f снимет протухшие маршруты и заведёт все цели заново, нужен sudo"))
		}
		state := ui.OK("\u25cf мимо VPN")
		switch {
		case len(dests) == 0:
			state = ui.Bad("\u2715 нет адресов")
		case t.bad > 0:
			state = ui.Warn(fmt.Sprintf("\u25b2 %d %s через туннель", t.bad, ui.Plural(t.bad, "адрес", "адреса", "адресов")))
		}
		t.row.Cells = ui.Pad(h.Addr, 26) + ui.Pad(ui.Dim(h.Label), 14) + ui.Pad(strconv.Itoa(len(dests)), 8) + state
		targets = append(targets, t)
	}
	sort.SliceStable(targets, func(i, j int) bool { return targets[i].bad > targets[j].bad })
	bypass := ui.OK(fmt.Sprintf("\u25cf обход %d из %d ", ok, total)) + ui.Dim("адресов идут мимо VPN")
	if ok < total {
		bypass += "   " + ui.Warn(fmt.Sprintf("\u25b2 %d через туннель", total-ok)) + ui.Dim("  f - починить")
	}
	d.Head = append(d.Head, bypass, "",
		ui.Dim(ui.Pad("ЦЕЛЬ", 26)+ui.Pad("ПОМЕТКА", 14)+ui.Pad("АДРЕСОВ", 8)+"СОСТОЯНИЕ"))
	for _, t := range targets {
		d.Rows = append(d.Rows, t.row)
	}
	return d
}

// ---------- dev-серверы ----------

func serveScreen() tui.Screen {
	return tui.NewBoard(tui.BoardOpts{
		Crumbs: []string{"Проекты", "Dev-серверы"},
		Command: func(row *tui.BoardRow) string {
			if row != nil {
				return "tim serve logs " + row.ID
			}
			return "tim serve list"
		},
		Load:  func(bool) tui.BoardData { return serveData() },
		Every: 5 * time.Second,
		Actions: []tui.BoardAction{
			{Key: "enter", Label: "браузер", Run: func(row *tui.BoardRow) (string, error) {
				st, ok := stateOf(row)
				if !ok {
					return "", ui.Errorf("сервера нет")
				}
				url := "http://localhost:" + st.Port
				return "открыто " + url, openBrowser(url)
			}},
			{Key: "s", Label: "стоп/старт", Run: func(row *tui.BoardRow) (string, error) {
				st, ok := stateOf(row)
				if !ok {
					return "", ui.Errorf("сервера нет")
				}
				if st.Alive() {
					if err := runner.StopGroup(st.PID); err != nil {
						return "", err
					}
					st.Remove()
					return st.Name + " остановлен", nil
				}
				return startAgain(st)
			}},
			{Key: "p", Label: "перезапуск", Run: func(row *tui.BoardRow) (string, error) {
				st, ok := stateOf(row)
				if !ok {
					return "", ui.Errorf("сервера нет")
				}
				if st.Alive() {
					if err := runner.StopGroup(st.PID); err != nil {
						return "", err
					}
					st.Remove()
				}
				return startAgain(st)
			}},
			{Key: "l", Label: "журнал", Do: func(row *tui.BoardRow) tui.Action {
				if row == nil {
					return tui.Action{}
				}
				name := row.ID
				return tui.Action{Push: tui.NewResult("журнал "+name, func() (string, error) {
					data, err := os.ReadFile(run.LogPath(name))
					if err != nil {
						return "", ui.Errorf("журнала %s нет", name)
					}
					lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
					if len(lines) > 300 {
						lines = lines[len(lines)-300:]
					}
					return strings.Join(lines, "\n"), nil
				}, tui.ResultOpts{Copy: copyText})}
			}},
		},
	})
}

func stateOf(row *tui.BoardRow) (run.State, bool) {
	if row == nil {
		return run.State{}, false
	}
	return run.LoadState(row.ID)
}

// startAgain: тот же проект на том же порту, вывод команды - в тост, а не на экран.
func startAgain(st run.State) (string, error) {
	p, err := cfg.Pick(st.Name)
	if err != nil {
		return "", err
	}
	var serr error
	_ = ui.Capture(func() { serr = serveBackground([]*config.Project{p}, st.Port) })
	if serr != nil {
		return "", serr
	}
	return st.Name + " запущен", nil
}

func serveData() tui.BoardData {
	d := tui.BoardData{Empty: "фоновых серверов нет: tim serve <проект> -d"}
	states := run.ListStates()
	alive := 0
	for _, s := range states {
		row := tui.BoardRow{ID: s.Name, Title: "журнал " + s.Name, Sub: "хвост, l - целиком"}
		age := human(time.Since(s.Started))
		if s.Alive() {
			alive++
			row.Cells = ui.OK("\u25cf ") + ui.Pad(s.Name, 18) + ui.Pad(":"+s.Port, 8) + ui.Dim(ui.Pad(s.Command, 24)+ui.Pad(age, 9)) + ui.Info("http://localhost:"+s.Port)
		} else {
			row.Cells = ui.Bad("\u2715 ") + ui.Pad(s.Name, 18) + ui.Pad(":"+s.Port, 8) + ui.Dim(ui.Pad(s.Command, 24)) + ui.Bad("умер") + ui.Dim("  s - запустить снова")
		}
		row.Pane = logTail(run.LogPath(s.Name), 6)
		d.Rows = append(d.Rows, row)
	}
	d.Status = fmt.Sprintf("%d %s", alive, ui.Plural(alive, "работает", "работают", "работают"))
	if len(states) == 0 {
		d.Status = "нет фоновых"
	}
	return d
}

// logTail: последние строки журнала, ошибки красным.
func logTail(path string, n int) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return []string{ui.Dim("журнала пока нет")}
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		l = clip(strings.TrimRight(l, "\r"), 110)
		low := strings.ToLower(l)
		switch {
		case strings.Contains(low, "error") || strings.Contains(low, "exception") || strings.Contains(low, " 500 ") || strings.Contains(low, "fatal"):
			out = append(out, ui.Bad(l))
		case strings.Contains(low, "warn"):
			out = append(out, ui.Warn(l))
		default:
			out = append(out, l)
		}
	}
	if len(out) == 0 {
		return []string{ui.Dim("журнал пуст")}
	}
	return out
}

// ---------- проверка проектов ----------

func checkScreen() tui.Screen {
	var (
		mu   sync.Mutex
		rows []health.Row
		only bool
	)
	return tui.NewBoard(tui.BoardOpts{
		Crumbs:  []string{"Проекты", "Проверка"},
		Loading: "обхожу проекты: git, сайт, сертификат, почта, домен - несколько секунд",
		Command: func(row *tui.BoardRow) string {
			if row != nil {
				return "tim check " + row.ID
			}
			return "tim check"
		},
		Load: func(fresh bool) tui.BoardData {
			mu.Lock()
			defer mu.Unlock()
			if rows == nil || fresh {
				rows = checkAll()
			}
			return checkData(rows, only)
		},
		Actions: []tui.BoardAction{
			{Key: "enter", Label: "подробно", Do: func(row *tui.BoardRow) tui.Action {
				if row == nil {
					return tui.Action{}
				}
				return resultAction([]string{"check", row.ID})
			}},
			{Key: "e", Label: "только проблемы", Run: func(*tui.BoardRow) (string, error) {
				mu.Lock()
				only = !only
				mu.Unlock()
				if only {
					return "только проблемные; e - все", nil
				}
				return "все проекты", nil
			}},
		},
	})
}

// checkAll: все проекты реестра, как tim check без аргументов.
func checkAll() []health.Row {
	projects, err := checkProjects(nil)
	if err != nil {
		return nil
	}
	rows := make([]health.Row, len(projects))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i, p := range projects {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			rows[i] = checkRow(p)
		})
	}
	wg.Wait()
	kept := rows[:0]
	for _, r := range rows {
		if r.Level() != health.Skip {
			kept = append(kept, r)
		}
	}
	health.Sort(kept)
	return kept
}

func checkData(rows []health.Row, only bool) tui.BoardData {
	var d tui.BoardData
	ok, warn, bad := health.Counts(rows)
	d.Status = fmt.Sprintf("%d %s", len(rows), ui.Plural(len(rows), "проект", "проекта", "проектов"))
	if warn+bad > 0 {
		d.Status += fmt.Sprintf(" \u00b7 %d требуют внимания", warn+bad)
	} else {
		d.Status += " \u00b7 всё в порядке"
	}
	cols := checkCols[:0:0]
	for _, col := range checkCols {
		for _, r := range rows {
			if r.Get(col.name).Level != health.Skip {
				cols = append(cols, col)
				break
			}
		}
	}
	head := ui.Pad("ПРОЕКТ", 20)
	for _, col := range cols {
		head += ui.Pad(strings.ToUpper(col.name), col.width)
	}
	d.Head = []string{ui.Dim(head)}
	hidden := 0
	for _, r := range rows {
		if only && r.Level() < health.Warn {
			hidden++
			continue
		}
		row := tui.BoardRow{ID: r.Name, Title: r.Name}
		if p, err := cfg.Pick(r.Name); err == nil && p.URL != "" {
			row.Sub = p.URL
		}
		row.Cells = ui.Pad(r.Name, 20)
		for _, col := range cols {
			c := r.Get(col.name)
			row.Cells += ui.Pad(lamp(c.Level, clip(c.Text, col.width-3)), col.width)
		}
		for _, c := range r.Problems() {
			text := c.Detail
			if text == "" {
				text = c.Text
			}
			row.Pane = append(row.Pane, lamp(c.Level, c.Name+": "+text))
		}
		if len(row.Pane) == 0 {
			row.Pane = []string{ui.OK("\u25cf всё в порядке")}
		}
		row.Pane = append(row.Pane, ui.Dim("\u2192 enter покажет проверку целиком, tim domain "+row.Sub+" - домен"))
		d.Rows = append(d.Rows, row)
	}
	if hidden > 0 {
		d.Foot = append(d.Foot, ui.Dim(fmt.Sprintf("ещё %d в порядке, e - показать", hidden)))
	}
	d.Foot = append(d.Foot, checkMachine()...)
	d.Empty = fmt.Sprintf("проблем нет: ок %d", ok)
	if len(rows) == 0 {
		d.Empty = "проверять нечего: ни у одного проекта нет git и адреса сайта"
	}
	return d
}
