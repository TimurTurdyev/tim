package sites

import (
	"sort"
	"strings"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// mailTab: почта домена по частям. У строки с бедой под ней - почему и что сделать, y копирует запись для DNS.
func mailTab(env *app.Env, domain string) tui.Tab {
	records := map[string]string{} // строка -> запись DNS, которую добавить
	return tui.NewBoard(tui.BoardOpts{
		Crumbs: []string{"Сайты", domain}, Tab: "Почта",
		Command: func(*tui.BoardRow) string { return "tim mail " + domain },
		Load: func(bool) tui.BoardData {
			d, recs := mailData(domain)
			clear(records)
			for k, v := range recs {
				records[k] = v
			}
			return d
		},
		Actions: []tui.BoardAction{
			{Key: "enter", Label: "подробно", Free: true, Do: func(*tui.BoardRow) tui.Action { return env.Result([]string{"mail", domain}) }},
			{Key: "y", Label: "скопировать", When: func(row *tui.BoardRow) bool { return records[row.ID] != "" },
				Run: func(row *tui.BoardRow) (string, error) {
					// В панели DNS имя и значение - разные поля: копируется значение, имя видно в строке.
					rec := records[row.ID]
					host, value, _ := strings.Cut(rec, ` TXT "`)
					runner.ToClipboard(strings.TrimSuffix(value, `"`))
					return "скопировано значение TXT для " + host, nil
				}},
		},
	}).(tui.Tab)
}

// mailData: строки почты и записи DNS, которые стоит добавить, по строкам.
func mailData(domain string) (tui.BoardData, map[string]string) {
	m := inspect.MailCheck(domain, inspect.DKIMSelectors)
	verdict, _ := m.Verdict()
	d := tui.BoardData{Tab: "Почта", Status: "почта: " + mailWord(verdict)}
	records := map[string]string{}
	if noMail(m) {
		d.Status = "почты нет"
		d.Empty = "Почты на домене нет. Нужна - подключи домен в Яндекс 360, Mail.ru или Google и добавь MX, SPF, DKIM, DMARC; " +
			"не нужна - закрой от подделки: MX 0 ., SPF -all, DMARC p=reject. Пошагово - enter"
		return d, records
	}
	d.Columns = ui.Pad("ЗАПИСЬ", 10) + "ЗНАЧЕНИЕ"
	d.Wide = true
	fixes := mailFixes(domain, m)
	add := func(name, value string, lvl level.Level, why string, extra ...string) {
		if value == "" {
			value = "нет"
		}
		row := tui.BoardRow{ID: name, Cells: ui.Pad(level.Lamp(lvl, name), 10) + value}
		var parts []string
		if why = strings.TrimPrefix(why, name+" нет: "); why != "" && lvl >= level.Warn {
			parts = append(parts, why)
		}
		// Запись раньше пояснения: строка под строкой обрезается по ширине, а запись - то, что копируют.
		var dos []string
		for _, f := range fixes {
			if f.Part != name {
				continue
			}
			dos = append(dos, f.Do)
			if f.Record != "" && records[name] == "" {
				parts = append(parts, f.Record)
				records[name] = f.Record
			}
		}
		parts = append(parts, dos...)
		parts = append(parts, extra...)
		if len(parts) > 0 && (lvl >= level.Warn || records[name] != "") {
			row.Inline = strings.Join(parts, "; ")
		}
		d.Rows = append(d.Rows, row)
	}

	switch {
	case m.NullMX:
		add("MX", "почты нет (null MX)", level.OK, "")
	case len(m.MX) == 0:
		add("MX", "", level.Bad, "письма на домен не приходят")
	default:
		add("MX", strings.Join(m.MX, ", "), level.OK, "")
	}
	if lvl, text := mxBlacklist(m.MX); lvl != level.Skip {
		why := ""
		if lvl == level.Bad {
			why = "письма с этих адресов отклоняют; узнать причину и снять - страница списка по адресу сервера"
		}
		add("списки", text, lvl, why)
	}
	spfLvl, spfWhy := worst(m.SPFF)
	add("SPF", m.SPF, spfLvl, spfWhy)
	stray := ""
	if m.DMARC == "" && len(m.DMARCOther) > 0 {
		stray = "на _dmarc ещё " + ui.Count(len(m.DMARCOther), "чужая запись", "чужие записи", "чужих записей") + " - их положили не туда"
	}
	dmLvl, dmWhy := worst(m.DMRF)
	if stray != "" {
		add("DMARC", m.DMARC, dmLvl, dmWhy, stray)
	} else {
		add("DMARC", m.DMARC, dmLvl, dmWhy)
	}
	var keys []string
	for sel := range m.DKIM {
		keys = append(keys, sel)
	}
	sort.Strings(keys)
	dkLvl, dkWhy := level.OK, ""
	if len(keys) == 0 {
		dkLvl, dkWhy = level.Warn, "по частым селекторам не найден: без подписи письма чаще уходят в спам"
	}
	add("DKIM", strings.Join(keys, ", "), dkLvl, dkWhy)
	return d, records
}

// worst: худшая находка и её текст - он же причина под строкой.
func worst(fs []inspect.Finding) (level.Level, string) {
	lvl, why := level.OK, ""
	for _, f := range fs {
		if f.Level > lvl {
			lvl, why = f.Level, f.Text
		}
	}
	return lvl, why
}
