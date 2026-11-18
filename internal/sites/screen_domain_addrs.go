package sites

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/TimurTurdyev/tim/internal/baseline"
	"github.com/TimurTurdyev/tim/internal/domaudit"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Колонки адресов: на 80 колонок влезают все, IPv6 режется - целиком он в карточке.
const (
	addrCol  = 18
	ownerCol = 25
	placeCol = 7
	portsCol = 16
	barsMax  = 3 // строк сетей и стран над списком: на 24 строки должен остаться сам список
	barCol   = 10
)

// addrsTab: адреса домена - сайты, почта и серверы имён.
func addrsTab(domain string, load *domainLoad, command func(*tui.BoardRow) string, actions ...tui.BoardAction) tui.Tab {
	return tui.NewBoard(tui.BoardOpts{
		Crumbs: []string{"Сайты", domain}, Tab: "Адреса", Search: true,
		Command: command, Wait: load.wait,
		Load:    func(fresh bool) tui.BoardData { rep, _ := load.get(fresh); return addrsData(rep) },
		Actions: actions,
	}).(tui.Tab)
}

func addrsData(rep *domaudit.Report) tui.BoardData {
	d := tui.BoardData{Status: ui.Count(len(rep.Addrs), "адрес", "адреса", "адресов"), Tab: "Адреса " + strconv.Itoa(len(rep.Addrs)),
		Head: shares(rep), Wide: true, Empty: "Адресов нет"}
	d.Columns = ui.Pad("АДРЕС", addrCol) + ui.Pad("СЕТЬ", ownerCol) + ui.Pad("ГДЕ", placeCol) + ui.Pad("ПОРТЫ", portsCol) + "ИМЁН"
	_, fresh := outsideNew(rep.Domain)
	groups := []struct {
		name string
		rows []tui.BoardRow
	}{{name: "НОВЫЙ ПОРТ СНАРУЖИ"}, {name: "САЙТЫ"}, {name: "ПОЧТА"}, {name: "DNS"}}
	for _, a := range rep.Addrs {
		row, i := addrRow(a), 1
		switch {
		case len(fresh[a.IP]) > 0:
			i = 0
			row.ID = newID + kindPort + " " + a.IP
			row.Card.Facts = slices.Insert(row.Card.Facts, 0, newPortFact(a.IP, fresh[a.IP]))
		case len(a.Hosts) > 0:
		case len(a.MX) > 0:
			i = 2
		default:
			i = 3
		}
		groups[i].rows = append(groups[i].rows, row)
	}
	for _, g := range groups {
		for _, row := range g.rows {
			row.Group = g.name + " · " + strconv.Itoa(len(g.rows))
			d.Rows = append(d.Rows, row)
		}
	}
	return d
}

func addrRow(a domaudit.Addr) tui.BoardRow {
	owner := ownerText(a)
	ports := ""
	if a.Services != nil {
		ports = portList(a.Services.Ports)
	}
	return tui.BoardRow{ID: a.IP, Find: strings.Join([]string{a.IP, owner, a.PTR, strings.Join(a.Hosts, " ")}, " "),
		Cells: ui.Fit(a.IP, addrCol-1) + " " + ui.Fit(owner, ownerCol-1) + " " + ui.Pad(placeText(a), placeCol) +
			ui.Dim(ui.Fit(ports, portsCol-1)) + " " + fmt.Sprintf("%4d", len(a.Hosts)),
		Card: addrCard(a, owner)}
}

func newPortFact(ip string, items []baseline.Item) tui.Fact {
	var ports []string
	for _, it := range items {
		ports = append(ports, strings.TrimPrefix(it.Key, ip+":"))
	}
	return tui.Fact{Label: "новое", Value: ui.Warn("порт "+strings.Join(ports, ", ")) + ui.Dim(" · "+appearedText(items[0])+" · не твой - закрыть в файрволе")}
}

func addrCard(a domaudit.Addr, owner string) *tui.Card {
	card := &tui.Card{Title: a.IP, Sub: owner}
	add := func(label, value string) {
		if value != "" {
			card.Facts = append(card.Facts, tui.Fact{Label: label, Value: value})
		}
	}
	if a.Internal {
		add("внутри", "адрес не из интернета: имя показывает устройство сети, убрать из общего DNS")
	}
	if s := a.Services; s != nil {
		add("порты", portList(s.Ports)+ui.Dim(" · по данным Shodan, до недели назад"))
		add("что там", strings.Join(s.Products, ", "))
		if n := len(s.CVE); n > 0 {
			add("CVE", fmt.Sprintf("%d по номеру версии", n)+ui.Dim(" · Debian и Ubuntu чинят, не меняя номер"))
		}
	}
	if len(a.Hosts) > 0 {
		add("имён", strconv.Itoa(len(a.Hosts))+ui.Dim(" · "+strings.Join(cut(a.Hosts, 4), ", ")))
	}
	add("почта", strings.Join(a.MX, ", "))
	add("DNS", strings.Join(a.NS, ", "))
	if a.AS.Prefix != "" {
		add("подсеть", a.AS.Prefix+ui.Dim(" · AS"+a.AS.ASN))
	}
	add("PTR", a.PTR)
	if a.CDN {
		add("CDN", "настоящий сервер за ней не виден")
	}
	if a.Own {
		add("соседи", neighborText(a, 6))
	}
	return card
}

// shares: у кого и где стоят адреса - полосами в две колонки: слева сети, справа страны.
func shares(rep *domaudit.Report) []string {
	total := len(rep.Addrs)
	if total == 0 {
		return nil
	}
	bar := func(name string, width, n int) string {
		return ui.Fit(name, width) + " " + ui.Progress(float64(n)/float64(total), barCol, 0) + " " + ui.Dim(strconv.Itoa(n))
	}
	networks, countries := domaudit.Networks(rep), domaudit.Countries(rep)
	slices.SortStableFunc(networks, func(a, b domaudit.Network) int { return b.Addrs - a.Addrs })
	slices.SortStableFunc(countries, func(a, b domaudit.Country) int { return b.Addrs - a.Addrs })
	var out []string
	for i := range min(barsMax, max(len(networks), len(countries))) {
		line := ""
		if i < len(networks) {
			line = bar(networkName(networks[i]), 22, networks[i].Addrs)
		}
		if i < len(countries) {
			line = ui.Pad(line, 40) + bar(countryName(countries[i]), 10, countries[i].Addrs)
		}
		out = append(out, line)
	}
	return out
}

func countryName(c domaudit.Country) string {
	switch {
	case c.CDN:
		return "везде"
	case c.Code == "":
		return "неизвестно"
	}
	return c.Code
}
