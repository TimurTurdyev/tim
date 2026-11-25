package sites

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/domaudit"
	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/par"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/servers"
	"github.com/TimurTurdyev/tim/internal/ui"
	"github.com/TimurTurdyev/tim/internal/view"
	"github.com/TimurTurdyev/tim/report"
)

// domain: отчёт по домену - экраном в терминале, иначе строками, JSON, страницей с картой или таблицей.
func domain(env *app.Env, arg string, o domainOptions) error {
	if o.all {
		return printDomains(env.Cfg)
	}
	name, err := DomainOf(env, arg)
	if err != nil {
		return err
	}
	if o.twins {
		return printTwins(name, o.json)
	}
	opt := domaudit.Options{Quiet: o.fast || o.own, Quick: o.fast, FreshCRT: o.fresh, SkipNeighbors: o.noNeighbors,
		Scan: o.scan || !o.scanSet && !o.fast && ownSite(env.Cfg, name), Direct: directIface(env.Cfg, o.direct)}
	opt.Inside = insideOf(env, name)
	if app.Interactive() && !o.json && !o.openMap && o.mapOut == "" && !o.fast {
		return env.Open(domainScreen(env, name, opt))
	}
	if !o.json {
		opt.Notify = func(s string) { ui.Println(ui.Dim(s)) }
	}
	if o.json {
		return view.Show(report.Report{Schema: "tim.domain/1", Data: collectDomain(name, opt)}, true)
	}
	var rep *domaudit.Report
	var about domainAbout
	par.All(func() { rep = collectDomain(name, opt) }, func() { about = aboutDomain(name, opt.Quiet) })
	if err := view.Show(domainReport(rep, about), false); err != nil {
		return err
	}
	return saveDomain(rep, o)
}

// ownSite: домен из sites конфига - свой, его серверы проверяем без вопросов.
func ownSite(cfg *config.Config, domain string) bool {
	return slices.Contains(roundDomains(siteHosts(cfg, nil)), inspect.Registrable(domain))
}

// insideOf: что серверы домена из sites сами знают о своих сайтах - по сохранённому обходу, без захода по ssh;
// у чужого домена спрашивать некого.
func insideOf(env *app.Env, domain string) func(ips []string) []domaudit.Inside {
	if !ownSite(env.Cfg, domain) {
		return nil
	}
	return func(ips []string) []domaudit.Inside {
		var out []domaudit.Inside
		for _, in := range servers.InsideNames(env, ips) {
			out = append(out, domaudit.Inside{Server: in.Alias, Names: in.Names, Wild: in.Wild, At: in.At})
		}
		return out
	}
}

// directIface: интерфейс для запросов DNS мимо VPN - по флагу или по [domain] direct конфига; пусто - обычным путём.
func directIface(cfg *config.Config, flag bool) string {
	if !flag && !cfg.Domain.Direct {
		return ""
	}
	svc, _ := netinfo.PhysIface()
	return svc.Device
}

// collectDomain: полный сбор и сравнение с прошлым разом; сбор без сторонних сервисов видит не всё, с ним не сравниваем.
func collectDomain(name string, opt domaudit.Options) *domaudit.Report {
	rep := domaudit.Collect(name, opt)
	if !opt.Quiet {
		observeOutside(rep, time.Now())
	}
	return rep
}

// saveDomain: --out по расширению - .xlsx таблицей, остальное страницей; --map открывает страницу в браузере.
func saveDomain(rep *domaudit.Report, o domainOptions) error {
	if strings.EqualFold(filepath.Ext(o.mapOut), ".xlsx") {
		if err := domaudit.WriteXLSX(rep, o.mapOut); err != nil {
			return ui.Errorf("таблица не записана: %v", err)
		}
		ui.Println(ui.Label("таблица ") + o.mapOut)
		o.mapOut = ""
	}
	if o.openMap || o.mapOut != "" {
		return writeMap(rep, o.mapOut, o.openMap)
	}
	return nil
}

// tableName: имя файла таблицы рядом, как у отчёта tim audit.
func tableName(domain string, now time.Time) string {
	return "domain-" + domain + "-" + now.Format("2006-01-02") + ".xlsx"
}

// DomainOf: домен из аргумента - имя, адрес сайта или url; имя на национальном языке - в том виде, в каком оно в DNS.
func DomainOf(env *app.Env, arg string) (string, error) {
	if arg == "" {
		return "", ui.Errorf("укажи домен: tim domain example.com")
	}
	h, _ := netinfo.HostPort(arg, "")
	t, err := netinfo.ParseTarget(h)
	if err != nil || t.Kind != netinfo.KindDomain {
		return "", ui.Errorf("%s - не домен: tim domain example.com", arg)
	}
	return t.Value, nil
}

func writeMap(rep *domaudit.Report, path string, open bool) error {
	if path == "" {
		path = filepath.Join(os.TempDir(), "tim-domain-"+rep.Domain+".html")
	}
	if err := domaudit.WriteMap(rep, path); err != nil {
		return err
	}
	ui.Println(ui.Label("карта ") + path)
	if !open {
		return nil
	}
	return runner.OpenBrowser(path)
}

// cut: первые n и "и ещё N".
func cut(list []string, n int) []string {
	if len(list) <= n {
		return list
	}
	return append(list[:n:n], "и ещё "+strconv.Itoa(len(list)-n))
}

// printDomains: кратко по всем доменам сайтов.
func printDomains(cfg *config.Config) error {
	domains := roundDomains(siteHosts(cfg, nil))
	if len(domains) == 0 {
		return ui.Errorf("сайтов нет: добавь их в конфиг - sites = [\"example.com\"]")
	}
	sort.Strings(domains)
	for _, d := range domains {
		rep := domaudit.Collect(d, domaudit.Options{Quiet: true, Quick: true})
		live := 0
		for _, h := range rep.Hosts {
			if h.Status > 0 {
				live++
			}
		}
		ui.Println(ui.Label(ui.Pad(d, 30)) + " имён " + ui.Pad(strconv.Itoa(len(rep.Hosts)), 4) +
			" живых " + ui.Pad(strconv.Itoa(live), 4) + " адресов " + strconv.Itoa(len(rep.Addrs)))
	}
	return nil
}
