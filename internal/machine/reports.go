package machine

import (
	"context"
	"errors"
	"fmt"
	"net"
	"runtime"
	"strconv"
	"time"

	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/procs"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/sshx"
	"github.com/TimurTurdyev/tim/internal/sysinfo"
	"github.com/TimurTurdyev/tim/internal/ui"
	"github.com/TimurTurdyev/tim/internal/view"
	"github.com/TimurTurdyev/tim/report"
)

// batteryData: батарея для --json; Present - есть ли она вообще.
type batteryData struct {
	Present      bool   `json:"present"`
	Charge       int    `json:"charge,omitempty"`
	Charging     bool   `json:"charging,omitempty"`
	Full         bool   `json:"full,omitempty"`
	Charger      string `json:"charger,omitempty"`
	Cycles       int    `json:"cycles,omitempty"`
	DesignCycles int    `json:"design_cycles,omitempty"`
	Capacity     int    `json:"capacity,omitempty"`
	Condition    string `json:"condition,omitempty"`
}

func batteryReport() report.Report {
	b, ok := ReadBattery()
	if !ok {
		return report.Report{Schema: "tim.battery/1", Data: batteryData{}, Blocks: []report.Block{report.Note("батареи нет")}}
	}
	d := batteryData{Present: true, Charge: b.Charge, Charging: b.Charging, Full: b.Full, Charger: b.Charger,
		Cycles: b.Cycles, Capacity: b.Capacity, Condition: b.Condition}
	charge := strconv.Itoa(b.Charge) + "%"
	switch {
	case b.Charging:
		charge += ", заряжается"
	case b.Full:
		charge += ", заряжена"
	}
	facts := report.Facts{{Key: "заряд", Value: charge}, {Key: "зарядка", Value: b.Charger}}
	cycles := strconv.Itoa(b.Cycles)
	if runtime.GOOS == "darwin" {
		d.DesignCycles = sysinfo.DesignCycles
		cycles += " из ~" + strconv.Itoa(sysinfo.DesignCycles) + " по ресурсу Apple"
	}
	facts = append(facts, report.Fact{Key: "циклы", Value: cycles})
	if b.Capacity > 0 {
		capacity := report.Fact{Key: "ёмкость", Value: strconv.Itoa(b.Capacity) + "% от заводской"}
		// Ниже 80% Apple сама предлагает замену: дальше батарея садится заметно быстрее.
		if b.Capacity < 80 {
			capacity.Value, capacity.Level = capacity.Value+", Apple считает это поводом для замены", report.Warn
		}
		facts = append(facts, capacity)
	}
	if b.Condition != "" {
		cond := report.Fact{Key: "состояние", Value: b.Condition}
		if b.Condition != "Good" {
			cond.Level = report.Warn
		}
		facts = append(facts, cond)
	}
	return report.Report{Schema: "tim.battery/1", Data: d, Blocks: []report.Block{nonEmpty(facts)}}
}

// wifiData: подключение к Wi-Fi для --json; Signal - dBm на macOS, проценты на Linux (SignalUnit).
type wifiData struct {
	Connected  bool   `json:"connected"`
	Iface      string `json:"iface,omitempty"`
	SSID       string `json:"ssid,omitempty"`
	Channel    string `json:"channel,omitempty"`
	PHY        string `json:"phy,omitempty"`
	RateMbps   int    `json:"rate_mbps,omitempty"`
	Signal     int    `json:"signal,omitempty"`
	SignalUnit string `json:"signal_unit,omitempty"`
	Noise      int    `json:"noise,omitempty"`
	Quality    string `json:"quality,omitempty"`
	Security   string `json:"security,omitempty"`
}

func wifiReport() (report.Report, error) {
	r := report.Report{Schema: "tim.wifi/1", Data: wifiData{}}
	w, ok, err := readWifi()
	switch {
	case err != nil:
		return r, err
	case !ok:
		r.Blocks = []report.Block{report.Note("к Wi-Fi не подключено")}
		return r, nil
	}
	d := wifiData{Connected: true, Iface: w.Iface, SSID: w.SSID, Channel: w.Channel, PHY: w.PHY, RateMbps: w.Rate,
		Signal: w.Signal, Security: w.Security}
	ssid := report.Fact{Key: "сеть", Value: w.SSID + "  " + w.Iface}
	if w.SSID == "<redacted>" {
		ssid = report.Fact{Key: "сеть", Value: "имя скрыто macOS: дай терминалу доступ к геолокации (Настройки - Конфиденциальность - Службы геолокации)", Level: report.Info}
	}
	facts := report.Facts{ssid, {Key: "канал", Value: w.Channel}}
	if runtime.GOOS == "linux" {
		d.SignalUnit = "%"
		facts = append(facts, report.Fact{Key: "скорость", Value: strconv.Itoa(w.Rate) + " Мбит/с"},
			report.Fact{Key: "сигнал", Value: strconv.Itoa(w.Signal) + "%"})
	} else {
		d.SignalUnit, d.Noise, d.Quality = "dBm", w.Noise, w.Quality()
		sig := report.Fact{Key: "сигнал", Value: fmt.Sprintf("%d dBm, шум %d dBm  %s", w.Signal, w.Noise, d.Quality)}
		if d.Quality == "слабый" || d.Quality == "плохой" {
			sig.Level = report.Warn
		}
		facts = append(facts, report.Fact{Key: "стандарт", Value: w.PHY + "  " + strconv.Itoa(w.Rate) + " Мбит/с"}, sig)
	}
	facts = append(facts, report.Fact{Key: "защита", Value: w.Security})
	r.Data, r.Blocks = d, []report.Block{nonEmpty(facts)}
	return r, nil
}

// readWifi: на macOS - system_profiler, на Linux - nmcli; без nmcli Wi-Fi смотреть нечем (на сервере его нет).
func readWifi() (sysinfo.Wifi, bool, error) {
	if runtime.GOOS == "linux" {
		if !runner.Has("nmcli") {
			return sysinfo.Wifi{}, false, ui.Errorf("nmcli не найден: Wi-Fi смотреть нечем (на сервере его обычно и нет)")
		}
		out, _ := runner.Output("nmcli", "-t", "-f", "ACTIVE,SSID,CHAN,RATE,SIGNAL,SECURITY", "dev", "wifi")
		w, ok := sysinfo.ParseNmcliWifi(out)
		return w, ok, nil
	}
	out, err := runner.Output("system_profiler", "SPAirPortDataType", "-json")
	if err != nil {
		return sysinfo.Wifi{}, false, ui.Errorf("system_profiler: %v", err)
	}
	w, ok := sysinfo.ParseWifi([]byte(out))
	return w, ok, nil
}

// ipData: внешний адрес для --json.
type ipData struct {
	IP      string       `json:"ip"`
	Geo     *inspect.Geo `json:"geo,omitempty"`
	Path    string       `json:"path"`
	Tunnel  bool         `json:"tunnel"`
	Socks   *inspect.Geo `json:"socks,omitempty"`
	Reverse string       `json:"reverse,omitempty"`
}

func ipReport(socksPort int) (report.Report, error) {
	ip, err := netinfo.ExternalIP("")
	if err != nil {
		return report.Report{}, ui.Errorf("внешний IP не узнать: %v", err)
	}
	inet := netinfo.InternetIface()
	d := ipData{IP: ip, Tunnel: netinfo.IsTunnelIface(inet), Path: "напрямую через " + ui.Dash(inet)}
	if d.Tunnel {
		d.Path = "через туннель " + inet + " (VPN)"
	}
	if _, ok := sshx.ShuttlePID(); ok {
		d.Path += ", tunnel all"
	}
	outside := report.Fact{Key: "снаружи", Value: ip}
	if g, err := inspect.IPGeo(ip); err == nil {
		d.Geo, outside.Value = g, geoLine(g)
	} else {
		outside.Value += "  геолокация: " + err.Error()
	}
	facts := report.Facts{outside, {Key: "путь", Value: d.Path, Level: report.Info}}
	for _, t := range sshx.Known() {
		if t.Name == "socks" && t.Alive() {
			if g, err := geoVia("socks5://127.0.0.1:" + strconv.Itoa(socksPort)); err == nil {
				d.Socks = g
				facts = append(facts, report.Fact{Key: "через SOCKS", Value: geoLine(g)})
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if names, err := net.DefaultResolver.LookupAddr(ctx, ip); err == nil && len(names) > 0 {
		d.Reverse = names[0]
		facts = append(facts, report.Fact{Key: "обратная", Value: names[0], Level: report.Info})
	}
	return report.Report{Schema: "tim.ip/1", Data: d, Blocks: []report.Block{facts}}, nil
}

// netStepData: шаг проверки сети для --json.
type netStepData struct {
	Name  string       `json:"name"`
	Level report.Level `json:"level"`
	Text  string       `json:"text"`
}

// checkNet проходит сеть по шагам и называет первый упавший; отчёт печатается и тогда.
func checkNet() error {
	var steps []netStepData
	var facts report.Facts
	failed := ""
	for _, s := range netSteps() {
		res, err := s.run()
		st := netStepData{Name: s.name, Level: report.OK, Text: res}
		if w, ok := errors.AsType[warning](err); ok {
			st.Level, st.Text = report.Warn, string(w)
		} else if err != nil {
			st.Level, st.Text = report.Bad, err.Error()
			if failed == "" {
				failed = s.name
			}
		}
		steps = append(steps, st)
		facts = append(facts, report.Fact{Key: st.Name, Value: st.Text, Level: st.Level})
	}
	if err := view.Show(report.Report{Schema: "tim.net/1", Data: steps, Blocks: []report.Block{facts}}, view.AsJSON); err != nil {
		return err
	}
	if failed != "" {
		return ui.Errorf("первым упал шаг %q - начни с него", failed)
	}
	return nil
}

// appData: приложение и его процессы для --json у tim ps.
type appData struct {
	Name   string     `json:"name"`
	Memory int64      `json:"memory_bytes"`
	CPU    float64    `json:"cpu"`
	Uptime int64      `json:"uptime_seconds"`
	Procs  []procData `json:"procs"`
}

type procData struct {
	PID    string  `json:"pid"`
	CPU    float64 `json:"cpu"`
	Memory int64   `json:"memory_bytes"`
	Name   string  `json:"name"`
	Main   bool    `json:"main,omitempty"`
	State  string  `json:"state,omitempty"`
}

func foundReport(query string) (report.Report, error) {
	list, err := procs.All()
	if err != nil {
		return report.Report{}, err
	}
	apps := procs.Find(list, query)
	if len(apps) == 0 {
		return report.Report{}, ui.Errorf("ничего похожего на %s не запущено; все: tim top", query)
	}
	data := make([]appData, 0, len(apps))
	var blocks []report.Block
	for _, a := range apps {
		d := appData{Name: a.Name, Memory: a.RSS, CPU: a.CPU, Uptime: int64(a.Main.Uptime.Seconds())}
		for _, p := range a.Procs {
			d.Procs = append(d.Procs, procData{PID: p.PID, CPU: p.CPU, Memory: p.RSS, Name: RowName(p),
				Main: p.PID == a.Main.PID && a.IsApp, State: stateNote(p.State)})
		}
		data = append(data, d)
		blocks = append(blocks, appBlocks(a, 8)...)
	}
	blocks = append(blocks, report.Next{{Text: "закрыть", Command: "tim kill " + query}}, report.Note("по номеру: tim kill <pid>"))
	return report.Report{Schema: "tim.ps/1", Data: data, Blocks: blocks}, nil
}

// appBlocks: приложение заголовком и его процессы таблицей - самые занятые сверху, не больше limit.
func appBlocks(a procs.App, limit int) []report.Block {
	head := report.Heading(fmt.Sprintf("%s  процессов %d, память %s, CPU %.1f%%, работает %s",
		a.Name, len(a.Procs), ui.Bytes(a.RSS), a.CPU, ui.Duration(a.Main.Uptime)))
	t := report.Table{Columns: []report.Column{{Title: "PID", Kind: report.Number, Style: report.Muted},
		{Title: "CPU"}, {Title: "ПАМЯТЬ", Kind: report.Bytes}, {Title: "ЧТО"}, {Title: "", Style: report.Muted}}}
	sorted := busiestFirst(a.Procs)
	for i, p := range sorted {
		if i == limit {
			break
		}
		pid, _ := strconv.Atoi(p.PID)
		note := report.Mark{Text: stateNote(p.State), Level: report.Warn}
		if p.PID == a.Main.PID && a.IsApp {
			note = report.Mark{Text: "главный", Level: report.Info}
		}
		t.Rows = append(t.Rows, []any{pid, fmt.Sprintf("%.1f%%", p.CPU), p.RSS, ui.Clip(RowName(p), 48), note})
	}
	blocks := []report.Block{head, t}
	if len(sorted) > limit {
		blocks = append(blocks, report.Note(fmt.Sprintf("и ещё %d", len(sorted)-limit)))
	}
	return blocks
}

func printApp(a procs.App, limit int) { view.Print(report.Report{Blocks: appBlocks(a, limit)}) }

// portCheckData: проверка чужого порта для --json.
type portCheckData struct {
	Addr  string `json:"addr"`
	Open  bool   `json:"open"`
	Ms    int64  `json:"ms,omitempty"`
	Error string `json:"error,omitempty"`
}

func checkPorts(addrs []string) error {
	var rows []portCheckData
	var facts report.Facts
	failed := 0
	for _, a := range addrs {
		d, err := netinfo.Dial(a, inspect.Timeout)
		if err != nil {
			failed++
			rows = append(rows, portCheckData{Addr: a, Error: err.Error()})
			facts = append(facts, report.Fact{Key: a, Value: err.Error(), Level: report.Bad})
			continue
		}
		rows = append(rows, portCheckData{Addr: a, Open: true, Ms: d.Milliseconds()})
		facts = append(facts, report.Fact{Key: a, Value: fmt.Sprintf("открыт, %d мс", d.Milliseconds())})
	}
	if err := view.Show(report.Report{Schema: "tim.port.check/1", Data: rows, Blocks: []report.Block{facts}}, view.AsJSON); err != nil {
		return err
	}
	if failed > 0 {
		return ui.Errorf("закрыто: %d из %d", failed, len(addrs))
	}
	return nil
}

// nonEmpty: факты без пустых значений - строки без данных не показываются.
func nonEmpty(list report.Facts) report.Facts {
	out := list[:0]
	for _, f := range list {
		if f.Value != "" {
			out = append(out, f)
		}
	}
	return out
}
