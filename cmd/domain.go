package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/domaudit"
	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var (
	domFast    bool
	domFresh   bool
	domNoNeigh bool
	domJSON    bool
	domMap     bool
	domMapOut  string
	domAll     bool
)

// domainOf: аргумент, поле url проекта или проект текущей папки.
func domainOf(arg string) (string, error) {
	if arg != "" {
		if p, ok := cfg.Project(arg); ok && p.URL != "" {
			h, _ := hostPort(p.URL, "")
			return registrable(h), nil
		}
		h, _ := hostPort(arg, "")
		if !strings.Contains(h, ".") {
			return "", ui.Errorf("не домен и не проект с полем url: %s", arg)
		}
		return h, nil
	}
	p, err := projectFor("")
	if err != nil {
		return "", ui.Errorf("укажи домен: tim domain example.com")
	}
	if p.URL == "" {
		return "", ui.Errorf("у проекта %s нет поля url: tim domain <домен>", p.Name)
	}
	h, _ := hostPort(p.URL, "")
	return h, nil
}

var domainCmd = &cobra.Command{
	Use:   "domain [домен|проект] [--fast] [--map] [--json]",
	Short: "Проверить домен целиком: записи, поддомены, адреса, живость, сертификаты",
	Long: `Собирает по домену всё, что отдают публичные источники: регистрацию, записи DNS, поддомены из логов
сертификатов и перебором частых имён, адреса с провайдером и страной, живость каждого имени, сертификаты
и признаки связанных сайтов. Домен уходит в crt.sh, whois.cymru.com и (для соседей по IP) api.hackertarget.com.
Только публичные записи и обычный запрос главной страницы.`,
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: projectNames,
	RunE: func(cmd *cobra.Command, args []string) error {
		if domAll {
			return domainAll()
		}
		domain, err := domainOf(first(args))
		if err != nil {
			return err
		}
		notify := func(s string) { ui.Println(ui.Dim(s)) }
		if domJSON {
			notify = nil
		}
		rep := domaudit.Collect(domain, domaudit.Options{SkipCRT: domFast, FreshCRT: domFresh, SkipNeighbors: domNoNeigh, Notify: notify})
		if domJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(rep)
		}
		printDomain(rep)
		if domMap || domMapOut != "" {
			return writeMap(rep, domMap)
		}
		return nil
	},
}

func writeMap(rep *domaudit.Report, open bool) error {
	path := domMapOut
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
	return runner.Run("open", path)
}

func printDomain(rep *domaudit.Report) {
	ui.Println(ui.Info(rep.Domain))

	if d, err := inspect.Registration(rep.Domain); err == nil {
		line := ui.Label("регистратор ") + dashIfEmpty(d.Registrar)
		if !d.Expires.IsZero() {
			line += "  до " + daysLeft(d.Expires)
		}
		ui.Println(line)
		if len(d.Nameservers) > 0 {
			ui.Println(ui.Label("ns          ") + strings.Join(d.Nameservers, " "))
		}
	}

	m := inspect.MailCheck(rep.Domain, inspect.DKIMSelectors)
	switch {
	case m.NullMX:
		ui.Println(ui.Label("mx          ") + ui.Dim("null MX: домен почту не принимает"))
	case len(m.MX) == 0:
		ui.Println(ui.Label("mx          ") + ui.Dim("нет"))
	default:
		ui.Println(ui.Label("mx          ") + strings.Join(m.MX, ", "))
	}
	mailNote := []string{}
	if m.SPF == "" {
		mailNote = append(mailNote, ui.Warn("SPF нет"))
	}
	if m.DMARC == "" {
		mailNote = append(mailNote, ui.Warn("DMARC нет"))
	}
	if len(mailNote) > 0 {
		ui.Println(ui.Label("почта       ") + strings.Join(mailNote, "  ") + ui.Dim("  подробно: tim mail "+rep.Domain))
	}

	if len(rep.Hosts) == 0 {
		ui.Println(ui.Bad("ни одно имя не резолвится: домена нет, он не делегирован или DNS недоступен"))
		for _, e := range rep.Errors {
			ui.Println(ui.Warn("! ") + ui.Dim(e))
		}
		return
	}

	ui.Println()
	ui.Println(ui.Info("Адреса и имена"))
	for _, a := range rep.Addrs {
		head := ui.Label(ui.Pad(a.IP, 22))
		var parts []string
		if a.AS.Name != "" {
			parts = append(parts, a.AS.Name)
		}
		if a.AS.Country != "" {
			parts = append(parts, a.AS.Country)
		}
		if a.AS.ASN != "" {
			parts = append(parts, "AS"+a.AS.ASN)
		}
		if a.PTR != "" {
			parts = append(parts, a.PTR)
		}
		ui.Println(head + " " + ui.Dim(strings.Join(parts, "  ")))
		for _, name := range a.Hosts {
			ui.Println("   " + hostLine(rep, name))
		}
		if len(a.Shared) > 0 {
			ui.Println("   " + ui.Dim("на этом адресе ещё: "+strings.Join(cut(a.Shared, 8), " ")))
		}
	}

	dead := 0
	for _, h := range rep.Hosts {
		if h.Status == 0 {
			dead++
		}
	}
	ui.Println()
	ui.Println(ui.Dim("имён " + strconv.Itoa(len(rep.Hosts)) + ", адресов " + strconv.Itoa(len(rep.Addrs)) +
		", не отвечают по HTTP " + strconv.Itoa(dead)))

	if !rep.CRTCached.IsZero() {
		ui.Println(ui.Dim("список имён из кэша от " + rep.CRTCached.Format("02.01 15:04") + ", обновить: --fresh"))
	}
	if len(rep.Related) > 0 {
		ui.Println(ui.Label("в тех же сертификатах: ") + strings.Join(cut(rep.Related, 10), " "))
	}
	if len(rep.Trackers) > 0 {
		var t []string
		for _, x := range rep.Trackers {
			t = append(t, x.Kind+" "+x.ID)
		}
		ui.Println(ui.Label("счётчики:    ") + strings.Join(t, ", "))
		ui.Println(ui.Dim("             поиск сайтов с тем же счётчиком: https://publicwww.com/websites/" + rep.Trackers[0].ID + "/"))
	}
	for _, e := range rep.Errors {
		ui.Println(ui.Warn("! ") + ui.Dim(e))
	}
	if !domMap && domMapOut == "" {
		ui.Println(ui.Dim("карта в браузере: tim domain " + rep.Domain + " --map"))
	}
}

func hostLine(rep *domaudit.Report, name string) string {
	for _, h := range rep.Hosts {
		if h.Name != name {
			continue
		}
		if h.Status == 0 {
			return ui.Pad(name, 34) + " " + ui.Dim("не отвечает по HTTP  "+h.Source)
		}
		code := strconv.Itoa(h.Status)
		switch {
		case h.Status >= 500:
			code = ui.Bad(code)
		case h.Status >= 400:
			code = ui.Warn(code)
		default:
			code = ui.Label(code)
		}
		tail := []string{h.Server, h.Title}
		if h.Final != "" {
			tail = append(tail, "-> "+h.Final)
		}
		var rest []string
		for _, t := range tail {
			if t != "" {
				rest = append(rest, t)
			}
		}
		return ui.Pad(name, 34) + " " + code + " " + ui.Dim(strings.Join(rest, "  "))
	}
	return name
}

func cut(list []string, n int) []string {
	if len(list) <= n {
		return list
	}
	return append(list[:n:n], "и ещё "+strconv.Itoa(len(list)-n))
}

// domainAll: краткая сводка по всем доменам реестра.
func domainAll() error {
	var domains []string
	for _, p := range cfg.Projects {
		if p.URL != "" {
			h, _ := hostPort(p.URL, "")
			domains = append(domains, h)
		}
	}
	if len(domains) == 0 {
		return ui.Errorf("ни у одного проекта нет поля url: tim proj add --url")
	}
	sort.Strings(domains)
	for _, d := range domains {
		rep := domaudit.Collect(d, domaudit.Options{SkipCRT: true, SkipNeighbors: true})
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

func init() {
	f := domainCmd.Flags()
	f.BoolVar(&domFast, "fast", false, "без логов сертификатов, только перебор частых имён")
	f.BoolVar(&domFresh, "fresh", false, "обновить логи сертификатов, не брать из кэша")
	f.BoolVar(&domNoNeigh, "no-neighbors", false, "не спрашивать соседей по IP")
	f.BoolVar(&domJSON, "json", false, "вывод в JSON")
	f.BoolVar(&domMap, "map", false, "собрать карту в HTML и открыть")
	f.StringVar(&domMapOut, "map-out", "", "сохранить карту в файл, не открывая")
	f.BoolVar(&domAll, "all", false, "кратко по всем доменам реестра")
	root.AddCommand(domainCmd)
}
