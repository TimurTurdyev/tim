package bypass

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/routewatch"
	"github.com/TimurTurdyev/tim/internal/ui"
	"github.com/TimurTurdyev/tim/report"
)

// routeRow: адрес цели для --json; цель и пометка - у каждого адреса.
type routeRow struct {
	Target string       `json:"target"`
	Label  string       `json:"label,omitempty"`
	Dest   string       `json:"dest,omitempty"`
	State  string       `json:"state"`
	Level  report.Level `json:"level"`
	Port22 *bool        `json:"port22,omitempty"`
}

// routesData: куда идёт трафик и где каждая цель - форма --json у tim route.
type routesData struct {
	Iface     string     `json:"iface"`
	Gateway   string     `json:"gateway"`
	Internet  string     `json:"internet"`
	TunnelAll bool       `json:"tunnel_all"`
	Guard     string     `json:"guard"`
	Targets   []routeRow `json:"targets"`
}

// routesReport: куда идёт трафик и где каждая цель; probe - ещё проверить порт 22. Без all и probe в
// таблице только адреса с отклонением и итог: 725 одинаковых строк "в обход" ничего не говорят.
func routesReport(cfg *config.Config, probe, all bool) report.Report {
	phys, gw := netinfo.Physical()
	d := routesData{Iface: phys.Device, Gateway: gw, Internet: netinfo.InternetIface()}
	d.TunnelAll = netinfo.IsTunnelIface(d.Internet)
	guard, guardLevel := guardState()
	d.Guard = guard
	facts := report.Facts{{Key: "сеть", Value: phys.Device + " шлюз " + ui.Dash(gw)}}
	if phys.Device == "" {
		facts[0] = report.Fact{Key: "сеть", Value: "физический интерфейс не найден: сеть выключена?", Level: report.Warn}
	}
	inet := report.Fact{Key: "интернет", Value: "через " + ui.Dash(d.Internet)}
	if d.TunnelAll {
		inet.Value, inet.Level = inet.Value+" - VPN забирает весь трафик", report.Warn
	}
	facts = append(facts, inet)
	r := report.Report{Schema: "tim.route/1", Data: &d}
	if len(cfg.Hosts) == 0 {
		r.Blocks = []report.Block{facts, report.Note("целей обхода нет: tim direct <сайт|адрес|подсеть|as:N>")}
		return r
	}
	facts = append(facts, report.Fact{Key: "страж", Value: guard, Level: guardLevel})
	d.Targets = routeRows(cfg, phys, gw, probe)
	bad := 0
	for _, t := range d.Targets {
		if t.Level >= report.Warn {
			bad++
		}
	}
	full := all || probe
	if !full {
		state := report.Fact{Key: "обход", Value: fmt.Sprintf("%d из %d %s, целей %d", len(d.Targets)-bad, len(d.Targets),
			ui.Plural(len(d.Targets), "адреса", "адресов", "адресов"), len(cfg.Hosts)), Level: report.OK}
		if bad > 0 {
			state.Level = report.Warn
		}
		facts = append(facts, state)
	}
	r.Blocks = []report.Block{facts}
	if full || bad > 0 {
		r.Blocks = append(r.Blocks, routeTable(d.Targets, full, probe))
	}
	if !full {
		r.Blocks = append(r.Blocks, report.Note("весь список: tim route --all"))
	}
	return r
}

func routeRows(cfg *config.Config, phys netinfo.Service, gw string, probe bool) []routeRow {
	var rows []routeRow
	expanded := ExpandHosts(cfg.Hosts)
	for hi, h := range cfg.Hosts {
		if len(expanded[hi]) == 0 {
			rows = append(rows, routeRow{Target: h.Addr, Label: h.Label, State: "не раскрывается в адреса", Level: report.Bad})
			continue
		}
		for _, dest := range expanded[hi] {
			state, l := routeState(dest, phys, gw)
			row := routeRow{Target: h.Addr, Label: h.Label, Dest: dest, State: state, Level: l}
			if probe && !strings.Contains(dest, "/") {
				open := PortOpen(dest, "22")
				row.Port22 = &open
			}
			rows = append(rows, row)
		}
	}
	return rows
}

// routeTable: цель и пометка - только у первого адреса цели, чтобы список читался группами.
func routeTable(rows []routeRow, full, probe bool) report.Table {
	t := report.Table{Columns: []report.Column{{Title: "ЦЕЛЬ", Style: report.Key}, {Title: "ПОМЕТКА"}, {Title: "АДРЕС"}, {Title: "СОСТОЯНИЕ"}}}
	if probe {
		t.Columns = append(t.Columns, report.Column{Title: "ПОРТ 22"})
	}
	prev := ""
	for _, r := range rows {
		if !full && r.Level < report.Warn {
			continue
		}
		name, label := r.Target, r.Label
		if name == prev {
			name, label = "", ""
		}
		prev = r.Target
		line := []any{name, label, r.Dest, report.Mark{Text: r.State, Level: r.Level}}
		if probe && r.Port22 != nil {
			port := report.Mark{Text: "отвечает", Level: report.OK}
			if !*r.Port22 {
				port = report.Mark{Text: "молчит", Level: report.Bad}
			}
			line = append(line, port)
		}
		t.Rows = append(t.Rows, line)
	}
	return t
}

// guardData: служба стража и её отчёт - форма --json у tim route status.
type guardData struct {
	Installed bool               `json:"installed"`
	Running   bool               `json:"running"`
	Log       string             `json:"log"`
	Status    *routewatch.Status `json:"status,omitempty"`
}

// guardReport: служба стража, последняя проверка, починки и их причины.
func guardReport() report.Report {
	d := guardData{Installed: GuardInstalled(), Running: GuardRunning(), Log: GuardLog()}
	service := report.Fact{Key: "служба", Value: "работает, журнал " + d.Log, Level: report.OK}
	switch {
	case !d.Installed:
		service = report.Fact{Key: "служба", Value: "не установлена: sudo tim route install", Level: report.Warn}
	case !d.Running:
		service = report.Fact{Key: "служба", Value: "установлена, но не работает; журнал " + d.Log, Level: report.Bad}
	}
	r := report.Report{Schema: "tim.route.status/1", Data: &d}
	st, ok := GuardStatus()
	if !ok {
		r.Blocks = []report.Block{report.Facts{service}, report.Note("отчёта стража нет")}
		return r
	}
	d.Status = &st
	now := time.Now()
	fine := report.Fact{Key: "в порядке", Value: strconv.Itoa(st.OK) + " из " + strconv.Itoa(st.Want) + " маршрутов"}
	if st.OK < st.Want {
		fine.Level = report.Warn
	}
	facts := report.Facts{service,
		{Key: "проверка", Value: ui.Duration(now.Sub(st.LastCheck)) + " назад, запущен " + st.Started.Format("02.01 15:04")},
		{Key: "сеть", Value: st.Iface + " шлюз " + ui.Dash(st.Gateway)}, fine,
		{Key: "починок", Value: fmt.Sprintf("за сутки %d, за неделю %d", st.Since(now.Add(-24*time.Hour)), st.Since(now.AddDate(0, 0, -7)))}}
	if st.Error != "" {
		facts = append(facts, report.Fact{Key: "ошибка", Value: st.Error, Level: report.Bad})
	}
	r.Blocks = []report.Block{facts}
	if len(st.Repairs) > 0 {
		r.Blocks = append(r.Blocks, repairBlocks(st.Repairs, now)...)
	}
	return r
}

// repairBlocks: причины починок за неделю, частые сверху, и пять последних.
func repairBlocks(repairs []routewatch.Repair, now time.Time) []report.Block {
	var recent []routewatch.Repair
	for _, r := range repairs {
		if r.At.After(now.AddDate(0, 0, -7)) {
			recent = append(recent, r)
		}
	}
	sum := routewatch.Summary(recent)
	kinds := slices.Collect(maps.Keys(sum))
	slices.SortFunc(kinds, func(a, b routewatch.Kind) int {
		return cmp.Or(cmp.Compare(sum[b], sum[a]), cmp.Compare(a, b))
	})
	why := report.Table{Columns: []report.Column{{Title: "ЗА НЕДЕЛЮ", Kind: report.Number}, {Title: "ПРИЧИНА"}}}
	for _, k := range kinds {
		why.Rows = append(why.Rows, []any{sum[k], k.String()})
	}
	last := report.Table{Columns: []report.Column{{Title: "КОГДА", Style: report.Muted}, {Title: "АДРЕС"}, {Title: "ЧТО БЫЛО"}}}
	for _, r := range repairs[max(0, len(repairs)-5):] {
		last.Rows = append(last.Rows, []any{r.At.Format("02.01 15:04"), r.Dest, r.Reason})
	}
	return []report.Block{report.Heading("починки"), why, report.Heading("последние"), last}
}

// vpnData: туннели, подключения, куда идёт интернет, DNS и обход - форма --json у tim vpn.
type vpnData struct {
	Tunnels     []netinfo.Tunnel `json:"tunnels"`
	Connections []string         `json:"connections,omitempty"`
	Internet    string           `json:"internet"`
	Mode        string           `json:"mode"` // full - весь трафик в туннель, split - раздельный, direct - без VPN
	DNS         []string         `json:"dns,omitempty"`
	BypassOK    int              `json:"bypass_ok"`
	BypassTotal int              `json:"bypass_total"`
}

// vpnReport: туннели, подключения, куда идёт интернет, DNS и сколько целей идут мимо VPN.
func vpnReport(cfg *config.Config) report.Report {
	d := vpnData{Tunnels: netinfo.Tunnels(), Internet: netinfo.InternetIface(), DNS: netinfo.DNSServers()}
	var facts report.Facts
	hasV6 := false
	for _, t := range d.Tunnels {
		hasV6 = hasV6 || len(t.IPv6) > 0
		facts = append(facts, report.Fact{Key: t.Iface, Value: strings.Join(append(slices.Clone(t.IPv4), t.IPv6...), " ")})
	}
	if len(d.Tunnels) == 0 {
		facts = append(facts, report.Fact{Key: "туннели", Value: "нет", Level: report.Info})
	}
	for _, c := range netinfo.NetConns() {
		if c.State == "Connected" {
			d.Connections = append(d.Connections, c.Name)
			facts = append(facts, report.Fact{Key: "подключение", Value: c.Name})
		}
	}
	inet := report.Fact{Key: "интернет"}
	switch {
	case netinfo.IsTunnelIface(d.Internet):
		d.Mode, inet.Value, inet.Level = "full", "весь трафик идёт в туннель "+d.Internet, report.Warn
	case len(d.Tunnels) > 0:
		d.Mode, inet.Value = "split", "раздельный туннель, интернет через "+ui.Dash(d.Internet)
	default:
		d.Mode, inet.Value, inet.Level = "direct", "напрямую через "+ui.Dash(d.Internet), report.Info
	}
	facts = append(facts, inet)
	if len(d.DNS) > 0 {
		facts = append(facts, report.Fact{Key: "dns", Value: strings.Join(d.DNS, " ")})
	}
	d.BypassOK, d.BypassTotal = BypassCount(cfg)
	if d.BypassTotal > 0 {
		bypass := report.Fact{Key: "обход", Value: strconv.Itoa(d.BypassOK) + " из " + strconv.Itoa(d.BypassTotal) + " адресов", Level: report.OK}
		if d.BypassOK < d.BypassTotal {
			bypass.Value, bypass.Level = bypass.Value+": tim route fix", report.Warn
		}
		guard, l := guardState()
		facts = append(facts, bypass, report.Fact{Key: "страж", Value: guard, Level: l})
	}
	r := report.Report{Schema: "tim.vpn/1", Data: &d, Blocks: []report.Block{facts}}
	if hasV6 {
		r.Blocks = append(r.Blocks, report.Note("обход работает только для IPv4; IPv6 через туннель остаётся как есть"))
	}
	return r
}

// BypassCount: сколько адресов целей сейчас идут мимо VPN и сколько их всего.
func BypassCount(cfg *config.Config) (ok, total int) {
	phys, gw := netinfo.Physical()
	for _, dests := range ExpandHosts(cfg.Hosts) {
		for _, d := range dests {
			total++
			if _, bad := RouteState(d, phys, gw); !bad && Bypassed(d, phys, gw) {
				ok++
			}
		}
	}
	return ok, total
}
