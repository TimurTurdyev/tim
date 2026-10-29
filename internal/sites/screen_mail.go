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
	d.Columns = ui.Pad("ЗАПИСЬ", 10) + "ЗНАЧЕНИЕ"
	d.Wide = true
	records := map[string]string{}
	add := func(name, value string, lvl level.Level, why, fix, record string, extra ...string) {
		if value == "" {
			value = "нет"
		}
		row := tui.BoardRow{ID: name, Cells: ui.Pad(level.Lamp(lvl, name), 10) + value}
		var parts []string
		if why = strings.TrimPrefix(why, name+" нет: "); why != "" {
			parts = append(parts, why)
		}
		switch {
		case record != "":
			parts = append(parts, "добавить: "+record)
			records[name] = record
		case fix != "":
			parts = append(parts, fix)
		}
		parts = append(parts, extra...)
		if lvl >= level.Warn || lvl == level.Skip && len(parts) > 0 {
			row.Inline = strings.Join(parts, "; ")
		}
		d.Rows = append(d.Rows, row)
	}

	switch {
	case m.NullMX:
		add("MX", "почты нет (null MX)", level.OK, "", "", "")
	case len(m.MX) == 0:
		add("MX", "", level.Bad, "письма на домен не приходят", "нужна запись MX почтового сервера: Яндекс 360, Mail.ru, свой", "")
	default:
		add("MX", strings.Join(m.MX, ", "), level.OK, "", "", "")
	}
	if lvl, text := mxBlacklist(m.MX); lvl != level.Skip {
		why := ""
		if lvl == level.Bad {
			why = "письма с этих адресов отклоняют"
		}
		add("списки", text, lvl, why, "узнать причину и снять: страница списка по адресу сервера", "")
	}

	spfLvl, spfWhy := worst(m.SPFF)
	spfRecord := ""
	if m.SPF == "" && !m.NullMX {
		spfLvl, spfWhy = level.Bad, "без SPF письма от домена легко подделать, и почтовики режут настоящие"
		spfRecord = domain + ` TXT "v=spf1 include:<сервер отправки> ~all"`
	}
	add("SPF", m.SPF, spfLvl, spfWhy, "поправить запись TXT домена: tim mail "+domain+" расскажет подробно", spfRecord)

	dmLvl, dmWhy := worst(m.DMRF)
	dmRecord, stray := "", ""
	if m.DMARC == "" && !m.NullMX {
		dmRecord = "_dmarc." + domain + ` TXT "v=DMARC1; p=none; rua=mailto:dmarc@` + domain + `"`
		if len(m.DMARCOther) > 0 {
			stray = "на _dmarc ещё " + ui.Count(len(m.DMARCOther), "чужая запись", "чужие записи", "чужих записей") + " - их положили не туда"
		}
	}
	add("DMARC", m.DMARC, dmLvl, dmWhy, "", dmRecord, stray)

	var keys []string
	for sel := range m.DKIM {
		keys = append(keys, sel)
	}
	sort.Strings(keys)
	dkLvl, dkWhy := level.OK, ""
	if len(keys) == 0 {
		dkLvl, dkWhy = level.Warn, "по частым селекторам не найден: без подписи письма чаще уходят в спам"
	}
	add("DKIM", strings.Join(keys, ", "), dkLvl, dkWhy, "свой селектор: tim mail "+domain+" --selector <имя>; включается у почтового сервиса", "")
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
