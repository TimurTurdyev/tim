package servers

import (
	"time"

	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// AttacksScreen: кто ломится на сервер - находки по журналам, потом владельцы сетей.
func AttacksScreen(target string, since time.Duration) tui.Screen {
	return tui.NewBoard(tui.BoardOpts{
		Crumbs:  []string{"Сервер", target, "кто ломится"},
		Loading: "читаю журналы " + target + " за " + window(since) + ", только чтение",
		Command: func(*tui.BoardRow) string { return "tim server " + target + " --attacks" },
		Load:    func(bool) tui.BoardData { return attacksData(target, since) },
	})
}

func attacksData(target string, since time.Duration) tui.BoardData {
	var d tui.BoardData
	r, err := collectAttacks(target, since)
	if err != nil {
		d.Status = "не достучался"
		d.Head = []string{level.Lamp(level.Bad, target+": "+err.Error())}
		d.Empty = "отчёта нет"
		return d
	}
	d.Status = "за " + window(since)
	d.Head = []string{ui.Dim(sources(r.Attacks))}
	for _, c := range r.Findings {
		d.Rows = append(d.Rows, attackRow(c))
	}
	for _, o := range r.Owners[:min(8, len(r.Owners))] {
		name := ownerName(o)
		d.Rows = append(d.Rows, tui.BoardRow{ID: "owner " + name, Title: name,
			Cells: ui.Pad("  сеть", 16) + ui.Pad(ui.Clip(name, 44), 46) + ui.Dim(attemptsText(o)),
			Pane:  []string{name, attemptsText(o)}})
	}
	d.Empty = "в журналах ничего тревожного"
	return d
}

// attackRow: находка строкой; в панели подробность и с чего начать.
func attackRow(c health.Check) tui.BoardRow {
	row := tui.BoardRow{ID: c.Name + " " + c.Text, Title: c.Name + ": " + c.Text}
	if c.Level > level.OK {
		row.Sub = c.Level.String()
	}
	row.Cells = ui.Pad(level.Lamp(c.Level, ui.Pad(c.Name, 12)), 16) + c.Detail
	row.Pane = []string{c.Detail}
	if fix := attackFix(c); fix != "" {
		row.Pane = append(row.Pane, ui.Dim("\u2192 "+fix))
	}
	return row
}

func attackFix(c health.Check) string {
	switch {
	case c.Name == "вход" && c.Level == level.Bad:
		return "проверь, твой ли это вход: last -i; если нет - смени пароль, выключи вход по паролю и поставь fail2ban"
	case c.Name == "вход":
		return "вход по ключу надёжнее: ssh-copy-id, затем PasswordAuthentication no"
	case c.Name == "ssh" && c.Level == level.Warn:
		return "apt install fail2ban - банит перебор сразу; общие чёрные списки - CrowdSec"
	case c.Level == level.Skip:
		return "sudo usermod -aG adm,systemd-journal <пользователь> или смотреть под root"
	case c.Name == "сайт" && c.Level == level.Warn:
		return "tim audit <домен> проверит, файл это или заглушка"
	}
	return ""
}
