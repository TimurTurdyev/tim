package servers

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/baseline"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/srvinfo"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// ipPrefix: у строки адреса ID начинается с него - по нему x и y знают, кого банить.
const ipPrefix = "ip "

// AttacksScreen: кто ломится - вопрос экрана "мне надо что-то делать?": сверху лампы, потом удачные входы,
// потом те, кто бьёт именно этот сервер; массовые сканеры одной строкой.
func AttacksScreen(env *app.Env, target string, since time.Duration) tui.Screen {
	var last *attackReport
	plan := func(row *tui.BoardRow) (banPlan, string, error) {
		ip := strings.TrimPrefix(row.ID, ipPrefix)
		var a *srvinfo.Attacks
		if last != nil {
			a = &last.Attacks
		}
		p, err := planBan(target, ip, a)
		return p, ip, err
	}
	isIP := func(row *tui.BoardRow) bool { return strings.HasPrefix(row.ID, ipPrefix) }
	return tui.NewBoard(tui.BoardOpts{
		Crumbs:  []string{"Серверы", target, "кто ломится"},
		Loading: "читаю журналы " + target + " за " + window(since) + ", только чтение",
		Command: func(*tui.BoardRow) string { return "tim server " + target + " --attacks" },
		Load: func(bool) tui.BoardData {
			r, err := collectAttacks(target, since)
			if err == nil {
				last = r
			}
			return attacksData(target, since, r, err)
		},
		Palette: func() (string, []tui.CommandEntry) { return recipePalette(env, target) },
		Actions: []tui.BoardAction{
			{Key: "x", Label: "забанить", When: isIP, Do: func(row *tui.BoardRow) tui.Action {
				p, ip, err := plan(row)
				if err != nil {
					return tui.Action{Run: func() (string, error) { return "", err }}
				}
				return tui.Action{Confirm: &tui.Dialog{Title: "Забанить " + ip + " на " + target + "?",
					Lines:   []string{"Адрес не сможет подключаться к серверу " + p.Term + "."},
					Command: p.Command(target), Note: banNote(p), OK: "Забанить " + p.Term, Danger: true,
					Then: &tui.Action{Exit: func() {
						if err := ban(target, p); err != nil {
							ui.Println(ui.Bad("не забанил: " + err.Error()))
						}
					}}}}
			}},
			{Key: "y", Label: "скопировать", When: isIP, Run: func(row *tui.BoardRow) (string, error) {
				p, _, err := plan(row)
				if err != nil {
					return "", err
				}
				runner.ToClipboard(p.Command(target))
				return "скопировано: " + p.Command(target), nil
			}},
			{Key: "space", Label: "отметить своим", Doing: "отмечаю сеть своей",
				When: func(row *tui.BoardRow) bool { return isIP(row) && strings.Contains(row.Find, "новая сеть") },
				Run: func(row *tui.BoardRow) (string, error) {
					ip := strings.TrimPrefix(row.ID, ipPrefix)
					return "сеть " + srvinfo.Network(ip) + " своя", baseline.Accept(object(target), "login-net", srvinfo.Network(ip))
				}},
		},
	})
}

func attacksData(target string, since time.Duration, r *attackReport, err error) tui.BoardData {
	var d tui.BoardData
	if err != nil {
		d.Status = "не достучался"
		d.Head = []string{level.Lamp(level.Bad, target+": "+err.Error())}
		d.Empty = "отчёта нет"
		return d
	}
	logins := loginRows(target, r)
	d.Status = attacksStatus(since, r, logins)
	var lamps []string
	for _, c := range r.Findings {
		if c.Name != "вход" && c.Level != level.Skip && c.Level < level.Warn {
			lamps = append(lamps, level.Lamp(c.Level, c.Name+": "+c.Text))
		}
	}
	d.Head = []string{strings.Join(lamps, "   "), ui.Dim("журналы: " + sources(r.Attacks))}
	d.Columns = ui.Pad("АДРЕС", 25) + "ЧТО БЫЛО"
	d.Wide = true
	var warn []tui.BoardRow
	for _, c := range r.Findings {
		if c.Name != "вход" && (c.Level >= level.Warn || c.Level == level.Skip) {
			warn = append(warn, findingRow(c))
		}
	}
	for _, row := range warn {
		row.Group = "ВНИМАНИЕ · " + strconv.Itoa(len(warn))
		d.Rows = append(d.Rows, row)
	}
	for _, row := range logins {
		row.Group = "ВХОДЫ · " + strconv.Itoa(len(logins))
		d.Rows = append(d.Rows, row)
	}
	for _, a := range r.Targeted {
		d.Rows = append(d.Rows, tui.BoardRow{ID: ipPrefix + a.IP, Group: "ЦЕЛЕНАПРАВЛЕННО · " + strconv.Itoa(len(r.Targeted)) + " · не массовые сканеры",
			Cells:  ui.Pad(level.Lamp(level.Warn, a.IP), 25) + a.Doing() + ui.Dim(netNote(r, a.IP)+" · в списках нет"),
			Inline: "что делать: x - забанить; перебор паролей не страшен, если вход только по ключу"})
	}
	if n := r.Noise; n.IPs > 0 {
		text := fmt.Sprintf("%d %s, %d %s", n.IPs, ui.Plural(n.IPs, "адрес", "адреса", "адресов"), n.Attempts,
			ui.Plural(n.Attempts, "попытка", "попытки", "попыток"))
		why := "фон интернета"
		if len(n.Sources) > 0 {
			why = "массовые сканеры (" + strings.Join(n.Sources, ", ") + ")"
		}
		if !r.Lists {
			why = "списки шума не скачались - шум по малому числу попыток"
		}
		guard := ""
		if r.Attacks.Fail2ban || r.Attacks.CrowdSec {
			guard = " · защита работает"
		}
		d.Rows = append(d.Rows, tui.BoardRow{ID: "noise", Group: "ШУМ",
			Cells:  ui.Pad(level.Lamp(level.Info, "шум"), 25) + text + ui.Dim(" · "+why+guard),
			Inline: "ботам всё равно, чей сервер: делать ничего не нужно, если вход только по ключу"})
	}
	for _, o := range r.Owners[:min(5, len(r.Owners))] {
		name := ownerName(o)
		d.Rows = append(d.Rows, tui.BoardRow{ID: "owner " + name, Group: "ОТКУДА БОЛЬШЕ ВСЕГО",
			Cells: ui.Fit(name, 60) + "  " + ui.Dim(attemptsText(o))})
	}
	d.Empty = "в журналах ничего тревожного"
	return d
}

// loginRows: удачные входы - с перебора красные, из новой сети и по паролю янтарные.
func loginRows(target string, r *attackReport) []tui.BoardRow {
	failed := map[string]int{}
	for _, c := range r.Attacks.SSHByIP {
		failed[c.Key] = c.N
	}
	fresh := map[string]bool{}
	for _, it := range baseline.Pending(object(target)) {
		if it.Kind == "login-net" {
			fresh[it.Key] = true
		}
	}
	type agg struct {
		l    srvinfo.Login
		n    int
		last time.Time
	}
	var order []string
	by := map[string]*agg{}
	for _, l := range r.Attacks.Logins {
		k := l.IP + " " + l.User
		if by[k] == nil {
			by[k] = &agg{l: l}
			order = append(order, k)
		}
		by[k].n++
		by[k].last = l.Time
	}
	var rows []tui.BoardRow
	for _, k := range order {
		a := by[k]
		lvl, parts := level.OK, []string{a.l.User}
		if a.n > 1 {
			parts = append(parts, fmt.Sprintf("%d %s", a.n, ui.Plural(a.n, "вход", "входа", "входов")))
		}
		if !a.last.IsZero() {
			parts = append(parts, a.last.Local().Format("02.01 15:04"))
		}
		if a.l.Method != "publickey" {
			lvl, parts = level.Warn, append(parts, methodText(a.l.Method))
		}
		if n := failed[a.l.IP]; n > 0 {
			lvl, parts = level.Bad, append(parts, fmt.Sprintf("перед этим %d %s пароля", n, ui.Plural(n, "попытка", "попытки", "попыток")))
		}
		find := ""
		if fresh[srvinfo.Network(a.l.IP)] {
			lvl, find = max(lvl, level.Warn), "новая сеть"
			parts = append(parts, "впервые из этой сети")
		} else {
			parts = append(parts, "эта сеть уже была")
		}
		inline := "что делать: если это не ты - смени пароль, проверь новое на экране сервера, запрети вход по паролю"
		if lvl == level.OK {
			inline = "обычный вход по ключу из знакомой сети"
		}
		// Свой вход из знакомой сети банить не предлагаем: x на нём закрыл бы дорогу себе.
		id := ipPrefix + a.l.IP
		if lvl == level.OK {
			id = "login " + a.l.IP + " " + a.l.User
		}
		rows = append(rows, tui.BoardRow{ID: id, Find: find,
			Cells:  ui.Pad(level.Lamp(lvl, a.l.IP), 25) + strings.Join(parts, " · ") + ui.Dim(netNote(r, a.l.IP)),
			Inline: inline})
	}
	slices.SortStableFunc(rows, func(x, y tui.BoardRow) int { return rowLevel(y) - rowLevel(x) })
	return rows
}

func rowLevel(r tui.BoardRow) int {
	switch {
	case strings.Contains(r.Cells, "✕"):
		return 2
	case strings.Contains(r.Cells, "▲"):
		return 1
	}
	return 0
}

// netNote: " · выход Tor · AS60729 DE" - кто владеет адресом и в каких он списках.
func netNote(r *attackReport, ip string) string {
	out := ""
	for _, a := range r.Targeted {
		if a.IP == ip && len(a.Lists) > 0 {
			out += " · " + strings.Join(a.Lists, ", ")
		}
	}
	if n := r.Nets[ip]; n != "" {
		out += " · " + n
	}
	return out
}

// attacksStatus: "за 24 ч · 1 вход с перебора · 3 цели · 1 214 шума".
func attacksStatus(since time.Duration, r *attackReport, logins []tui.BoardRow) string {
	parts := []string{"за " + window(since)}
	bad := 0
	for _, row := range logins {
		if rowLevel(row) == 2 {
			bad++
		}
	}
	if bad > 0 {
		parts = append(parts, fmt.Sprintf("%d %s с перебора", bad, ui.Plural(bad, "вход", "входа", "входов")))
	}
	if n := len(r.Targeted); n > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", n, ui.Plural(n, "цель", "цели", "целей")))
	}
	if r.Noise.IPs > 0 {
		parts = append(parts, strconv.Itoa(r.Noise.IPs)+" шума")
	}
	return strings.Join(parts, " · ")
}
