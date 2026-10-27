package sites

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/baseline"
	"github.com/TimurTurdyev/tim/internal/ctlog"
	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/twins"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// newID: у строки нового ID - "new <вид> <ключ>", по нему space отмечает своим.
const newID = "new "

// acceptAction: space - новое домена своё; свой выпуск - и центр, который его выпустил.
func acceptAction(domain string) tui.BoardAction {
	return tui.BoardAction{Key: "space", Label: "отметить своим", Doing: "отмечаю своим",
		When: func(row *tui.BoardRow) bool { return strings.HasPrefix(row.ID, newID) },
		Run: func(row *tui.BoardRow) (string, error) {
			obj := domainObject(domain)
			kind, key, _ := strings.Cut(strings.TrimPrefix(row.ID, newID), " ")
			if kind == "ct" {
				for _, it := range baseline.Pending(obj) {
					if it.Kind == "ct" && it.Key == key {
						parts := strings.Split(it.Text, " · ")
						_ = baseline.Accept(obj, "ct-issuer", parts[len(parts)-1])
					}
				}
			}
			return "отмечено своим", baseline.Accept(obj, kind, key)
		}}
}

// recordsTab: записи домена; сменившиеся - группой сверху с прежним значением.
func recordsTab(domain string) tui.Tab {
	return tui.NewBoard(tui.BoardOpts{
		Crumbs: []string{"Сайты", domain}, Tab: "Записи", Search: true,
		Loading: "спрашиваю DNS " + domain,
		Command: func(*tui.BoardRow) string { return "tim dns " + domain },
		Load:    func(bool) tui.BoardData { return recordsData(domain, watchDomain(domain)) },
		Actions: []tui.BoardAction{acceptAction(domain)},
	}).(tui.Tab)
}

func recordsData(domain string, w domainWatch) tui.BoardData {
	var fresh []baseline.Item
	for _, it := range baseline.Pending(domainObject(domain)) {
		if strings.HasPrefix(it.Kind, "dns:") {
			fresh = append(fresh, it)
		}
	}
	d := tui.BoardData{Tab: "Записи " + strconv.Itoa(len(w.Records)), Status: ui.Count(len(w.Records), "запись", "записи", "записей")}
	if len(fresh) > 0 {
		d.Status += " · " + strconv.Itoa(len(fresh)) + " " + ui.Plural(len(fresh), "изменилась", "изменились", "изменились")
	}
	d.Columns = ui.Pad("ТИП", 12) + "ЗНАЧЕНИЕ"
	d.Wide = true
	changed := map[string]bool{}
	group := "ИЗМЕНИЛОСЬ" + sinceText(fresh) + " · " + strconv.Itoa(len(fresh))
	for _, it := range fresh {
		parts := strings.Split(it.Kind, ":")
		name := parts[len(parts)-1]
		if len(parts) == 3 {
			name = parts[1] + " " + name
		}
		text := it.Key
		if it.Prev != "" {
			text += ui.Dim(" · было " + it.Prev)
		}
		changed[it.Kind+" "+it.Key] = true
		d.Rows = append(d.Rows, tui.BoardRow{ID: newID + it.Kind + " " + it.Key, Group: group,
			Cells:  ui.Pad(level.Lamp(level.Warn, name), 12) + text + ui.Dim(" · "+seenText(it)),
			Inline: "что делать: " + recordFix(it.Kind)})
	}
	rest := 0
	for _, r := range w.Records {
		if !changed[r.kind()+" "+r.Value] {
			rest++
		}
	}
	for _, r := range w.Records {
		if changed[r.kind()+" "+r.Value] {
			continue
		}
		name := r.Type
		if r.Host != "" {
			name = r.Host + " " + r.Type
		}
		d.Rows = append(d.Rows, tui.BoardRow{ID: r.kind() + " " + r.Value, Group: "ЗАПИСИ · " + strconv.Itoa(rest),
			Cells: ui.Pad(name, 12) + r.Value})
	}
	if len(w.CAA) == 0 {
		d.Foot = append(d.Foot, ui.Dim("записи CAA нет: сертификат на домен может выпустить любой центр"))
	}
	d.Empty = "Записей нет: домен не делегирован или DNS недоступен"
	return d
}

func seenText(it baseline.Item) string {
	if it.After.IsZero() {
		return "замечено " + it.Seen.Local().Format("02.01 15:04")
	}
	return "сменилось между " + it.After.Local().Format("02.01 15:04") + " и " + it.Seen.Local().Format("02.01 15:04")
}

// certTab: сертификат домена и выпуски за 30 дней по журналам CT.
func certTab(domain string) tui.Tab {
	return tui.NewBoard(tui.BoardOpts{
		Crumbs: []string{"Сайты", domain}, Tab: "Сертификат",
		Loading: "сертификат " + domain + " и журналы выпусков",
		Command: func(*tui.BoardRow) string { return "tim cert " + domain },
		Load:    func(bool) tui.BoardData { return certData(domain, watchDomain(domain)) },
		Actions: []tui.BoardAction{acceptAction(domain),
			{Key: "w", Label: "браузер", Free: true, Doing: "открываю crt.sh", Run: func(*tui.BoardRow) (string, error) {
				return "crt.sh открыт в браузере", runner.OpenBrowser("https://crt.sh/?q=" + domain)
			}}},
	}).(tui.Tab)
}

func certData(domain string, w domainWatch) tui.BoardData {
	d := tui.BoardData{Tab: "Сертификат"}
	if info, err := inspect.Cert(domain, "443"); err == nil {
		leaf := info.Leaf()
		days := int(time.Until(leaf.NotAfter).Hours() / 24)
		lvl := level.OK
		switch {
		case days < 7 || info.VerifyErr != nil:
			lvl = level.Bad
		case days < 21:
			lvl = level.Warn
		}
		issuer := leaf.Issuer.CommonName
		if len(leaf.Issuer.Organization) > 0 {
			issuer += " · " + leaf.Issuer.Organization[0]
		}
		d.Head = []string{
			ui.Label(ui.Pad("кому", 12)) + strings.Join(cut(leaf.DNSNames, 4), ", "),
			ui.Label(ui.Pad("кем", 12)) + issuer,
			ui.Label(ui.Pad("срок", 12)) + leaf.NotBefore.Format("2006-01-02") + " - " + leaf.NotAfter.Format("2006-01-02") + " · " +
				level.Lamp(lvl, strconv.Itoa(days)+" дн"),
		}
		d.Status = "до " + leaf.NotAfter.Format("02.01")
	} else {
		d.Head = []string{level.Lamp(level.Bad, "сертификат не получен: "+err.Error())}
	}
	if w.CertErr != nil {
		d.Foot = append(d.Foot, ui.Warn("▲ журналы выпусков: "+w.CertErr.Error()))
		return d
	}
	pending := map[string]bool{}
	newIssuer := map[string]bool{}
	for _, it := range baseline.Pending(domainObject(domain)) {
		switch it.Kind {
		case "ct":
			pending[it.Key] = true
		case "ct-issuer":
			newIssuer[it.Key] = true
		}
	}
	var recent []ctlog.Issuance
	for _, c := range w.Certs {
		if time.Since(c.NotBefore) < 30*24*time.Hour {
			recent = append(recent, c)
		}
	}
	group := "ВЫПУЩЕНЫ ЗА 30 ДН · " + strconv.Itoa(len(recent))
	for _, c := range recent {
		lvl, mark, inline := level.OK, "", ""
		id := "ct " + c.ID
		if pending[c.ID] {
			mark, id = "+ ", newID+"ct "+c.ID
			inline = "что делать: если выпускал не ты - кто-то подтвердил домен; проверь DNS и сервер"
		}
		note := ""
		if newIssuer[c.Issuer] {
			lvl, note = level.Warn, " · этот центр раньше на домен не выпускал"
			inline = "что делать: не ты - кто-то подтвердил домен; запись CAA разрешит выпуск только твоему центру"
		}
		d.Rows = append(d.Rows, tui.BoardRow{ID: id, Group: group,
			Cells:  ui.Pad(level.Lamp(lvl, mark+ui.Clip(namesText(c.Names), 34)), 40) + c.NotBefore.Local().Format("02.01") + " · " + c.Issuer + ui.Dim(note),
			Inline: inline})
	}
	if n := len(w.Certs) - len(recent); n > 0 {
		d.Foot = append(d.Foot, ui.Dim(fmt.Sprintf("ещё %d %s раньше", n, ui.Plural(n, "действующий выпущен", "действующих выпущены", "действующих выпущены"))))
	}
	if len(w.CAA) == 0 {
		d.Foot = append(d.Foot, ui.Dim("записи CAA нет: выпустить может любой центр; запись issue \"letsencrypt.org\" оставит только его"))
	}
	d.Empty = "за 30 дней новых выпусков нет"
	return d
}

// twinsTab: двойники домена - считаются при первом открытии вкладки, это сотня запросов DNS.
func twinsTab(domain string) tui.Tab {
	return tui.NewBoard(tui.BoardOpts{
		Crumbs: []string{"Сайты", domain}, Tab: "Двойники", Search: true,
		Loading: "ищу двойники " + domain + ": опечатки, похожие буквы, другие зоны",
		Command: func(*tui.BoardRow) string { return "tim domain " + domain + " --twins" },
		Load:    func(bool) tui.BoardData { return twinsData(domain) },
		Actions: []tui.BoardAction{{Key: "w", Label: "браузер", Doing: "открываю", Run: func(row *tui.BoardRow) (string, error) {
			return row.ID + " открыт в браузере", runner.OpenBrowser("http://" + row.ID)
		}}},
	}).(tui.Tab)
}

func twinsData(domain string) tui.BoardData {
	cands := twins.Candidates(domain)
	found := twins.Registered(cands)
	d := tui.BoardData{Tab: "Двойники " + strconv.Itoa(len(found)),
		Status: fmt.Sprintf("%d из %d вариантов зарегистрированы", len(found), len(cands))}
	d.Columns = ui.Pad("ИМЯ", 32) + ui.Pad("КАК", 16) + "ЧТО ТАМ"
	d.Wide = true
	for _, t := range found {
		lvl, what := level.Info, ui.Dim("только зарегистрирован")
		if len(t.IPs) > 0 {
			what = strings.Join(cut(t.IPs, 2), ", ")
		}
		if t.MX {
			lvl, what = level.Warn, what+" · принимает почту"
		}
		group := "ПРИНИМАЮТ ПОЧТУ"
		if !t.MX {
			group = "ЗАРЕГИСТРИРОВАНЫ"
		}
		d.Rows = append(d.Rows, tui.BoardRow{ID: t.Name, Group: group,
			Cells:  ui.Pad(level.Lamp(lvl, t.Name), 32) + ui.Pad(t.How, 16) + what,
			Inline: "что делать: если не твой и похож на фишинг - жалоба регистратору; свой - пусть ведёт на основной сайт"})
	}
	sortTwinRows(d.Rows)
	d.Empty = "двойников не нашлось: ни один из " + strconv.Itoa(len(cands)) + " вариантов не зарегистрирован"
	return d
}

func sortTwinRows(rows []tui.BoardRow) {
	slices.SortStableFunc(rows, func(a, b tui.BoardRow) int {
		return strings.Compare(b.Group, a.Group) // ПРИНИМАЮТ ПОЧТУ раньше ЗАРЕГИСТРИРОВАНЫ
	})
}

// namesText: первое имя сертификата и сколько ещё.
func namesText(names []string) string {
	if len(names) == 0 {
		return "-"
	}
	if len(names) == 1 {
		return names[0]
	}
	return fmt.Sprintf("%s и ещё %d", names[0], len(names)-1)
}
