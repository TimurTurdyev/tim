package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/domaudit"
	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/run"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/srvinfo"
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

// ---------- домен ----------

func domainScreen(domain string) tui.Screen {
	var (
		mu  sync.Mutex
		rep *domaudit.Report
	)
	return tui.NewBoard(tui.BoardOpts{
		Crumbs:  []string{"Сайты", domain},
		Loading: "собираю по " + domain + ": записи, логи сертификатов, адреса, живость имён - до минуты",
		Command: func(*tui.BoardRow) string { return "tim domain " + domain },
		Load: func(fresh bool) tui.BoardData {
			mu.Lock()
			defer mu.Unlock()
			if rep == nil || fresh {
				rep = domaudit.Collect(domain, domaudit.Options{FreshCRT: fresh})
			}
			return domainData(rep)
		},
		Actions: []tui.BoardAction{
			{Key: "enter", Label: "подробно", Do: func(*tui.BoardRow) tui.Action { return resultAction([]string{"domain", domain, "--fast"}) }},
			{Key: "m", Label: "карта в браузере", Run: func(*tui.BoardRow) (string, error) {
				mu.Lock()
				defer mu.Unlock()
				if rep == nil {
					return "", ui.Errorf("отчёта ещё нет")
				}
				path := filepath.Join(os.TempDir(), "tim-domain-"+rep.Domain+".html")
				if err := domaudit.WriteMap(rep, path); err != nil {
					return "", err
				}
				return "карта открыта в браузере", openBrowser(path)
			}},
			{Key: "p", Label: "почта", Do: func(*tui.BoardRow) tui.Action { return resultAction([]string{"mail", domain}) }},
		},
	})
}

func domainData(rep *domaudit.Report) tui.BoardData {
	var d tui.BoardData
	d.Status = fmt.Sprintf("имён %d \u00b7 адресов %d", len(rep.Hosts), len(rep.Addrs))
	var lamps []string
	site := health.Site("https://" + rep.Domain)
	lamps = append(lamps, lamp(site.Level, "сайт "+site.Text))
	cert := health.Cert(rep.Domain)
	lamps = append(lamps, lamp(cert.Level, "сертификат "+cert.Text))
	if reg, err := inspect.Registration(rep.Domain); err == nil && !reg.Expires.IsZero() {
		c := health.Deadline("домен", reg.Expires, health.DomainWarnDays, health.DomainBadDays)
		lamps = append(lamps, lamp(c.Level, "домен "+c.Text)+ui.Dim("  "+reg.Registrar))
	}
	m := inspect.MailCheck(rep.Domain, inspect.DKIMSelectors)
	level, reasons := m.Verdict()
	switch {
	case m.NullMX:
		lamps = append(lamps, ui.Dim("- почта: домен её не принимает"))
	case level == "ok":
		lamps = append(lamps, ui.OK("\u25cf почта настроена"))
	case level == "warn":
		lamps = append(lamps, ui.Warn("\u25b2 почта: "+strings.Join(reasons, ", ")))
	default:
		lamps = append(lamps, ui.Bad("\u2715 почта: "+strings.Join(reasons, ", "))+ui.Dim("  письма могут уходить в спам"))
	}
	for i := 0; i+1 < len(lamps); i += 2 {
		d.Head = append(d.Head, ui.Pad(lamps[i], 52)+lamps[i+1])
	}
	if len(lamps)%2 == 1 {
		d.Head = append(d.Head, lamps[len(lamps)-1])
	}
	// Адреса апекса: по ним понятно, "тот же сервер" ли у поддомена.
	apex := map[string]bool{}
	for _, h := range rep.Hosts {
		if h.Name == rep.Domain {
			for _, ip := range h.IPs {
				apex[ip] = true
			}
		}
	}
	as := map[string]domaudit.Addr{}
	for _, a := range rep.Addrs {
		as[a.IP] = a
	}
	d.Head = append(d.Head, "", ui.Dim(ui.Pad("ИМЯ", 30)+ui.Pad("АДРЕС", 18)+ui.Pad("ОТВЕТ", 12)+"ЗАМЕТКА"))
	hosts := append([]domaudit.Host(nil), rep.Hosts...)
	sort.SliceStable(hosts, func(i, j int) bool {
		bad := func(h domaudit.Host) int {
			switch {
			case h.Name == rep.Domain:
				return -1
			case h.Status == 0 || h.Status >= 500:
				return 2
			case h.Status >= 400:
				return 1
			}
			return 0
		}
		return bad(hosts[i]) > bad(hosts[j]) && hosts[i].Name != rep.Domain || hosts[j].Name == rep.Domain && hosts[i].Name != rep.Domain && false
	})
	for _, h := range hosts {
		row := tui.BoardRow{ID: h.Name, Title: h.Name, Sub: h.Source}
		ip := "-"
		if len(h.IPs) > 0 {
			ip = h.IPs[0]
		}
		var status, note string
		switch {
		case h.Status == 0:
			status = ui.Bad("\u2715 молчит")
			note = "не отвечает по HTTP"
		case h.Status >= 500:
			status = ui.Bad(fmt.Sprintf("\u2715 %d", h.Status))
			note = "сервер отвечает ошибкой"
		case h.Status >= 400:
			status = ui.Warn(fmt.Sprintf("\u25b2 %d", h.Status))
		default:
			status = ui.OK(fmt.Sprintf("\u25cf %d", h.Status))
		}
		if a, ok := as[ip]; ok && note == "" {
			switch {
			case h.Name == rep.Domain:
				note = strings.TrimSpace(a.AS.Name + " " + a.AS.Country)
			case apex[ip]:
				note = "тот же сервер"
			case a.CDN:
				note = "за CDN " + a.AS.Name
			default:
				note = "другой сервер: " + strings.TrimSpace(a.AS.Name+" "+a.AS.Country)
			}
		}
		row.Cells = ui.Pad(h.Name, 30) + ui.Pad(ip, 18) + ui.Pad(status, 12) + ui.Dim(note)
		for _, ip := range h.IPs {
			line := ui.Pad(ip, 18)
			if a, ok := as[ip]; ok {
				line += ui.Dim(strings.TrimSpace(a.AS.Name + " " + a.AS.Country + " AS" + a.AS.ASN))
				if a.PTR != "" {
					line += ui.Dim("  " + a.PTR)
				}
			}
			row.Pane = append(row.Pane, line)
		}
		var about []string
		for _, t := range []string{h.Server, h.Title} {
			if t != "" {
				about = append(about, t)
			}
		}
		if h.Final != "" {
			about = append(about, "-> "+h.Final)
		}
		if len(about) > 0 {
			row.Pane = append(row.Pane, ui.Dim(strings.Join(about, "  ")))
		}
		if h.Status == 0 {
			row.Pane = append(row.Pane, ui.Warn("имя есть в DNS или логах сертификатов, а сайта нет: запись устарела?"))
		}
		d.Rows = append(d.Rows, row)
	}
	if len(rep.Related) > 0 {
		d.Foot = append(d.Foot, ui.Dim("в тех же сертификатах: "+strings.Join(cut(rep.Related, 8), " ")))
	}
	for _, e := range rep.Errors {
		d.Foot = append(d.Foot, ui.Warn("! "+e))
	}
	d.Empty = "ни одно имя не резолвится: домена нет, он не делегирован или DNS недоступен"
	return d
}

// ---------- сервер ----------

func serverScreen(target string) tui.Screen {
	return tui.NewBoard(tui.BoardOpts{
		Crumbs:  []string{"Серверы", target},
		Loading: "захожу по ssh на " + target + ", только чтение",
		Command: func(*tui.BoardRow) string { return "tim server " + target },
		Load:    func(bool) tui.BoardData { return serverData(target) },
		Actions: []tui.BoardAction{
			{Key: "s", Label: "ssh", Do: func(*tui.BoardRow) tui.Action {
				return tui.Action{Exit: func() { runAfter([]string{"ssh", target}) }}
			}},
			{Key: "enter", Label: "отчёт целиком", Do: func(*tui.BoardRow) tui.Action { return resultAction([]string{"server", target}) }},
		},
	})
}

func serverData(target string) tui.BoardData {
	var d tui.BoardData
	r, err := collectServer(target, 15*time.Second)
	if err != nil {
		d.Status = "не достучался"
		why := err.Error()
		if strings.Contains(why, "255") {
			why = "ssh не подключился: имя не резолвится, порт закрыт или ключ не подходит"
		}
		d.Head = []string{ui.Bad("\u2715 " + target + ": " + why), ui.Dim("маршрут и вход проверит tim ssh " + target + "; s - зайти по ssh")}
		d.Empty = "отчёта нет"
		return d
	}
	d.Status = summaryLine(r)
	barLine := func(label string, pct int, tail string) string {
		return ui.Pad(label, 10) + tui.Bar(float64(pct)/100, 30) + "  " + pctText(pct) + ui.Dim("  "+tail)
	}
	for _, disk := range r.Disks {
		if disk.Mount == "/" {
			d.Head = append(d.Head, barLine("диск", disk.Pct, fmtBytes(disk.Used)+" из "+fmtBytes(disk.Size)))
		}
	}
	if r.MemTotal > 0 {
		used := r.MemTotal - r.MemAvail
		tail := fmtBytes(used) + " из " + fmtBytes(r.MemTotal)
		if r.SwapTotal > 0 {
			tail += ", swap " + fmtBytes(r.SwapTotal-r.SwapFree)
		}
		d.Head = append(d.Head, barLine("память", int(used*100/r.MemTotal), tail))
	}
	if r.Cores > 0 {
		pct := int(r.Load[1] * 100 / float64(r.Cores))
		d.Head = append(d.Head, barLine("нагрузка", pct, fmt.Sprintf("%.2f за 5 мин, %d %s", r.Load[1], r.Cores, ui.Plural(r.Cores, "ядро", "ядра", "ядер"))))
	}
	d.Head = append(d.Head, "")
	var lamps []string
	if len(r.Failed) == 0 {
		lamps = append(lamps, ui.OK("\u25cf службы в порядке"))
	} else {
		lamps = append(lamps, ui.Bad("\u2715 упали: "+strings.Join(r.Failed, ", ")))
	}
	switch n := len(r.SecurityUpdates); {
	case n == 0 && !r.Reboot:
		lamps = append(lamps, ui.OK("\u25cf обновления безопасности стоят"))
	case n == 0:
		lamps = append(lamps, ui.Warn("\u25b2 нужна перезагрузка"))
	default:
		lamps = append(lamps, ui.Warn(fmt.Sprintf("\u25b2 %d %s безопасности ждут", n, ui.Plural(n, "обновление", "обновления", "обновлений"))))
	}
	switch {
	case r.NoSudoSSHD:
		lamps = append(lamps, ui.Dim("- sshd не проверен: нужен sudo без пароля"))
	case r.SSHD["permitrootlogin"] == "yes" || r.SSHD["passwordauthentication"] == "yes":
		lamps = append(lamps, ui.Warn("\u25b2 ssh: вход по паролю разрешён"))
	default:
		lamps = append(lamps, ui.OK("\u25cf ssh только по ключу"))
	}
	var open []string
	for _, l := range r.Listening {
		if l.Public {
			open = append(open, strconv.Itoa(l.Port))
		}
	}
	lamps = append(lamps, ui.OK("\u25cf наружу ")+ui.Dim(strings.Join(open, ", ")))
	switch {
	case r.Fail2ban == "active":
		lamps = append(lamps, ui.OK("\u25cf fail2ban работает"))
	case r.UFW == "active":
		lamps = append(lamps, ui.OK("\u25cf ufw включён"))
	default:
		lamps = append(lamps, ui.Warn("\u25b2 защиты от перебора не видно"))
	}
	for i := 0; i < len(lamps); i += 2 {
		line := ui.Pad(lamps[i], 46)
		if i+1 < len(lamps) {
			line += lamps[i+1]
		}
		d.Head = append(d.Head, line)
	}
	findings := srvinfo.Evaluate(r, time.Now())
	for _, c := range findings {
		row := tui.BoardRow{ID: c.Name + " " + c.Text, Title: c.Name + ": " + c.Text, Sub: c.Level.String()}
		row.Cells = ui.Pad(lamp(c.Level, ui.Pad(c.Name, 12)), 16) + clip(c.Detail, 90)
		row.Pane = []string{c.Detail}
		if fix := serverFix(c, r); fix != "" {
			row.Pane = append(row.Pane, ui.Dim("\u2192 "+fix))
		}
		d.Rows = append(d.Rows, row)
	}
	d.Empty = "замечаний нет"
	return d
}

// serverFix: с чего начать по каждой находке - команда или место, где смотреть.
func serverFix(c health.Check, r srvinfo.Report) string {
	switch c.Name {
	case "юнит":
		return "journalctl -u " + c.Text + " -n 50, потом systemctl restart " + c.Text
	case "обновления":
		return "apt update && apt upgrade"
	case "перезагрузка":
		return "reboot в тихое время; до неё работает старое ядро или libc"
	case "диск":
		return "du -xh --max-depth=2 " + strings.Fields(c.Text)[0] + " | sort -h | tail; journalctl --vacuum-size=500M; docker system prune"
	case "inode":
		return "чаще всего это кэш сессий или почта в очереди: find / -xdev -type d -size +1M"
	case "память", "swap":
		return "ps aux --sort=-rss | head; лишнее - остановить, иначе добавить память"
	case "нагрузка":
		return "top -o %CPU; долгая нагрузка - очереди, cron или сканеры в логах nginx"
	case "порт":
		return "слушать на 127.0.0.1 или закрыть ufw: ufw deny " + strings.Fields(c.Text)[0]
	case "sshd":
		return "в /etc/ssh/sshd_config: PermitRootLogin no, PasswordAuthentication no; затем systemctl reload ssh"
	case "защита":
		return "apt install fail2ban; ufw allow OpenSSH && ufw enable"
	case "nginx":
		return "nginx -t покажет строку с ошибкой"
	case "сертификат":
		return "certbot renew --dry-run; проверь таймер: systemctl status certbot.timer"
	}
	return ""
}
