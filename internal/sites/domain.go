package sites

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/domaudit"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/par"
	"github.com/TimurTurdyev/tim/internal/runner"
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
	if app.Interactive() && !o.json && !o.openMap && o.mapOut == "" && !o.fast {
		return env.Open(DomainScreen(env, name))
	}
	notify := func(s string) { ui.Println(ui.Dim(s)) }
	if o.json {
		notify = nil
	}
	opt := domaudit.Options{Quiet: o.fast, FreshCRT: o.fresh, SkipNeighbors: o.noNeighbors, Notify: notify}
	if o.json {
		return view.Show(report.Report{Schema: "tim.domain/1", Data: domaudit.Collect(name, opt)}, true)
	}
	var rep *domaudit.Report
	var about domainAbout
	par.All(func() { rep = domaudit.Collect(name, opt) }, func() { about = aboutDomain(name) })
	if err := view.Show(domainReport(rep, about), false); err != nil {
		return err
	}
	return saveDomain(rep, o)
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
		rep := domaudit.Collect(d, domaudit.Options{Quiet: true})
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
