package servers

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/baseline"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/srvinfo"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Сервер как место: Сайты, Пользователи и Входы - вкладки рядом с Состоянием. Сайты и пользователи - один
// обход по ssh на обе вкладки; открывается последний известный, r - заново.

// loginsWindow: за сколько показывать входы.
const loginsWindow = 7 * 24 * time.Hour

// explorer: общий обход для вкладок Сайты и Пользователи.
type explorer struct {
	target string
	mu     sync.Mutex
	e      *srvinfo.Explore
	at     time.Time
	err    error
}

func (x *explorer) get(fresh bool) (*srvinfo.Explore, time.Time, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.e == nil && !fresh {
		if e, at, ok := savedExplore(x.target); ok {
			x.e, x.at = &e, at
		}
	}
	if x.e == nil || fresh {
		e, err := collectExplore(x.target)
		if err != nil {
			x.err = err
			return x.e, x.at, err
		}
		x.e, x.at, x.err = &e, time.Now(), nil
	}
	return x.e, x.at, nil
}

func sitesTab(env *app.Env, x *explorer) tui.Tab {
	return tui.NewBoard(tui.BoardOpts{
		Crumbs: []string{"Серверы", x.target}, Tab: "Сайты", Search: true, Refresh: true,
		Loading: "читаю nginx и журналы " + x.target + ", только чтение",
		Order:   "по запросам за сутки",
		Command: func(*tui.BoardRow) string { return "tim server " + x.target + " --run sites" },
		Load: func(fresh bool) tui.BoardData {
			e, at, err := x.get(fresh)
			return sitesData(e, at, err)
		},
		Actions: []tui.BoardAction{
			{Key: "w", Label: "браузер", Doing: "открываю сайт", Run: func(row *tui.BoardRow) (string, error) {
				return row.ID + ": открыт в браузере", runner.OpenBrowser("https://" + row.ID)
			}},
			{Key: "t", Label: "трафик", Free: true, Do: func(*tui.BoardRow) tui.Action {
				return tui.Action{Push: TrafficScreen(env, x.target, 24*time.Hour)}
			}},
		},
	}).(tui.Tab)
}

func sitesData(e *srvinfo.Explore, at time.Time, err error) tui.BoardData {
	var d tui.BoardData
	if e == nil {
		d.Empty = "Не достучался: " + errText(err)
		return d
	}
	d.Status = ui.Count(len(e.Sites), "сайт", "сайта", "сайтов") + " · обход " + ago(at)
	if e.NoSudo {
		d.Status += " · без root видно не всё"
	}
	d.Columns = ui.Fit("САЙТ", 24) + ui.Fit("ПОЛЬЗОВАТЕЛЬ", 20) + ui.Fit("PHP", 6) + ui.Fit("СЕРТ", 9) + ui.Fit("ЗА СУТКИ", 10) + ui.Fit("5xx", 6) + "ФАЙЛЫ МЕНЯЛИ"
	d.Wide = true
	sites := append([]srvinfo.Site(nil), e.Sites...)
	slices.SortStableFunc(sites, func(a, b srvinfo.Site) int { return cmp.Compare(b.Hits, a.Hits) })
	for _, s := range sites {
		row := tui.BoardRow{ID: s.Name, Find: s.Name + " " + strings.Join(s.Aliases, " ") + " " + s.Owner,
			Cells: ui.Fit(s.Name, 24) + ui.Fit(ui.Dash(s.Owner), 20) + ui.Fit(cmpDash(s.PHP, s.Proxy), 6) + ui.Fit(certDays(s.CertEnd), 9) +
				ui.Fit(strconv.Itoa(s.Hits), 10) + ui.Fit(errorsCell(s.Errors), 6) + touchedCell(s.Touched)}
		row.Card = siteCard(s)
		d.Rows = append(d.Rows, row)
	}
	d.Empty = "Сайтов в конфиге nginx не нашлось"
	if e.NoSudo {
		d.Empty += ": конфиги сайтов часто читает только root, а sudo -n просит пароль"
	}
	return d
}

func siteCard(s srvinfo.Site) *tui.Card {
	c := &tui.Card{Title: s.Name, Sub: s.Root}
	fact := func(label, value string) {
		if value != "" {
			c.Facts = append(c.Facts, tui.Fact{Label: label, Value: value})
		}
	}
	fact("ещё имена", strings.Join(s.Aliases, ", "))
	fact("владелец", s.Owner)
	switch {
	case s.PHP != "":
		fact("PHP", s.PHP)
	case s.Proxy != "":
		fact("приложение", "nginx проксирует на "+s.Proxy)
	}
	if !s.CertEnd.IsZero() {
		fact("сертификат", "до "+s.CertEnd.Local().Format("02.01.2006")+ui.Dim(" · "+s.Cert))
	}
	fact("за сутки", strconv.Itoa(s.Hits)+" "+ui.Plural(s.Hits, "запрос", "запроса", "запросов")+", 5xx: "+strconv.Itoa(s.Errors))
	fact("журналы", strings.Join(s.Logs, ", "))
	fact("ошибки", s.ErrorLog)
	fact("конфиг", s.Conf)
	return c
}

func usersTab(target string, x *explorer) tui.Tab {
	return tui.NewBoard(tui.BoardOpts{
		Crumbs: []string{"Серверы", target}, Tab: "Пользователи", Search: true, Refresh: true,
		Loading: "читаю пользователей " + target + ", только чтение",
		Command: func(*tui.BoardRow) string { return "tim server " + target + " --run who" },
		Load: func(fresh bool) tui.BoardData {
			e, at, err := x.get(fresh)
			return usersData(target, e, at, err)
		},
	}).(tui.Tab)
}

func usersData(target string, e *srvinfo.Explore, at time.Time, err error) tui.BoardData {
	var d tui.BoardData
	if e == nil {
		d.Empty = "Не достучался: " + errText(err)
		return d
	}
	fresh := map[string]bool{}
	for _, it := range baseline.Pending(object(target)) {
		if it.Kind == "user" {
			name, _, _ := strings.Cut(it.Key, " ")
			fresh[name] = true
		}
	}
	d.Status = ui.Count(len(e.Accounts), "пользователь", "пользователя", "пользователей") + " с входом · обход " + ago(at)
	d.Columns = ui.Fit("ПОЛЬЗОВАТЕЛЬ", 20) + ui.Fit("КЕМ ЗАВЕДЁН", 16) + ui.Fit("КОГДА", 12) + ui.Fit("КЛЮЧИ", 7) + ui.Fit("ВХОДИЛ", 16) + "САЙТЫ"
	d.Wide = true
	for _, a := range e.Accounts {
		when := "-"
		if !a.Created.IsZero() {
			when = a.Created.Local().Format("02.01.2006")
		}
		if fresh[a.Name] {
			when = ui.Warn("новый, " + strings.TrimSuffix(when, "."+strconv.Itoa(time.Now().Year())))
		}
		keys := ui.Dim("нет")
		if a.Keys > 0 {
			keys = strconv.Itoa(a.Keys)
		}
		login := ui.Dim("не входил")
		if !a.LastLogin.IsZero() {
			login = a.LastLogin.Local().Format("02.01 15:04")
		}
		by := a.By
		if by == "" {
			by = ui.Warn("вручную")
		}
		d.Rows = append(d.Rows, tui.BoardRow{ID: a.Name, Find: a.Name + " " + strings.Join(a.Sites, " "),
			Cells: ui.Fit(a.Name, 20) + ui.Fit(by, 16) + ui.Fit(when, 12) + ui.Fit(keys, 7) + ui.Fit(login, 16) + ui.Dim(strings.Join(a.Sites, ", ")),
			Card:  accountCard(a)})
	}
	d.Empty = "Пользователей с входом не нашлось"
	return d
}

func accountCard(a srvinfo.Account) *tui.Card {
	c := &tui.Card{Title: a.Name, Sub: a.Home}
	fact := func(label, value string) {
		if value != "" {
			c.Facts = append(c.Facts, tui.Fact{Label: label, Value: value})
		}
	}
	switch {
	case a.By != "" && len(a.Sites) > 0:
		fact("откуда", "заведён "+a.By+" вместе с сайтом "+strings.Join(a.Sites, ", "))
	case a.By != "":
		fact("откуда", "заведён: "+a.By)
	default:
		fact("откуда", ui.Warn("заведён вручную")+ui.Dim(" · не панелью; если не ты - кто входил: a"))
	}
	if !a.Created.IsZero() {
		fact("заведён", a.Created.Local().Format("02.01.2006 15:04"))
	}
	fact("группы", strings.Join(a.Groups, ", "))
	fact("оболочка", a.Shell)
	fact("ключи ssh", strconv.Itoa(a.Keys))
	if !a.LastLogin.IsZero() {
		fact("входил", a.LastLogin.Local().Format("02.01.2006 15:04")+ui.Dim(" · "+a.LastFrom))
	}
	fact("процессов", strconv.Itoa(a.Procs))
	return c
}

// loginsTab: удачные входы по ssh за неделю - кто и откуда; тот же разбор, что "кто ломится".
func loginsTab(env *app.Env, target string) tui.Tab {
	return tui.NewBoard(tui.BoardOpts{
		Crumbs: []string{"Серверы", target}, Tab: "Входы", Search: true,
		Loading: "читаю журналы входов " + target + " за неделю, только чтение",
		Command: func(*tui.BoardRow) string { return "tim server " + target + " --attacks --since 7d" },
		Load: func(bool) tui.BoardData {
			var d tui.BoardData
			r, err := collectAttacks(target, loginsWindow)
			if err != nil {
				d.Empty = "Не прочитал журналы: " + errText(err)
				return d
			}
			d.Rows = loginRows(target, r)
			d.Status = ui.Count(len(d.Rows), "адрес", "адреса", "адресов") + " с удачным входом за неделю"
			d.Columns = ui.Fit("ОТКУДА", 25) + "КТО · СКОЛЬКО · КОГДА"
			d.Wide = true
			d.Empty = "За неделю по ssh никто не входил"
			if r.Attacks.SSH != srvinfo.Read {
				d.Empty = "Журнал входов не прочитать: нужен root или sudo без пароля"
			}
			return d
		},
		Actions: []tui.BoardAction{
			{Key: "a", Label: "кто ломится", Free: true, Do: func(*tui.BoardRow) tui.Action {
				return tui.Action{Push: AttacksScreen(env, target, 24*time.Hour)}
			}},
		},
	}).(tui.Tab)
}

func certDays(end time.Time) string {
	if end.IsZero() {
		return ui.Dim("-")
	}
	days := ui.DaysUntil(end, time.Now())
	text := strconv.Itoa(days) + " дн"
	switch {
	case days < 7:
		return level.Lamp(level.Bad, text)
	case days < 21:
		return ui.Warn(text)
	}
	return text
}

func errorsCell(n int) string {
	if n == 0 {
		return ui.Dim("0")
	}
	return ui.Warn(strconv.Itoa(n))
}

func touchedCell(t time.Time) string {
	if t.IsZero() {
		return ui.Dim("-")
	}
	if d := time.Since(t); d >= 24*time.Hour {
		return ui.Dim(ui.Short(d))
	}
	return ui.Dim("сегодня")
}

func cmpDash(a, b string) string {
	switch {
	case a != "":
		return a
	case b != "":
		return "прокси"
	}
	return ui.Dim("-")
}

func errText(err error) string {
	if err == nil {
		return "нет ответа"
	}
	return sshError(err)
}
