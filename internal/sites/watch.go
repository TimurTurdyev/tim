package sites

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/baseline"
	"github.com/TimurTurdyev/tim/internal/ctlog"
	"github.com/TimurTurdyev/tim/internal/dnsq"
	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/twins"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Домен сравнивается с прошлым разом: записи DNS и выпуски сертификатов. Чужая смена MX или NS и
// сертификат от незнакомого центра - первые признаки угона домена.

// recordTypes: порядок типов записей на вкладке.
var recordTypes = []string{"A", "AAAA", "CNAME", "MX", "NS", "TXT", "CAA"}

// record: запись домена; Host - пусто у самого домена, "www" у www.
type record struct {
	Host, Type, Value string
}

func (r record) kind() string {
	if r.Host == "" {
		return "dns:" + r.Type
	}
	return "dns:" + r.Host + ":" + r.Type
}

// domainWatch: что знаем о домене сейчас.
type domainWatch struct {
	Records []record
	Certs   []ctlog.Issuance
	CertErr error
	CAA     []string
}

func domainObject(domain string) string { return "domain-" + domain }

// records: записи домена и www через системный резолвер, CAA - отдельным запросом.
func records(domain string) ([]record, []string) {
	var out []record
	for _, host := range []string{"", "www"} {
		name := domain
		if host != "" {
			name = host + "." + domain
		}
		for _, r := range inspect.DNS(name, "") {
			// У www NS и TXT - те же, что у домена, или их нет: шум.
			if host == "" || r.Type == "A" || r.Type == "AAAA" || r.Type == "CNAME" {
				out = append(out, record{host, r.Type, r.Value})
			}
		}
	}
	caa, _ := dnsq.CAA(domain)
	for _, c := range caa {
		out = append(out, record{"", "CAA", c})
	}
	return out, caa
}

// watchDomain: записи и сертификаты домена - и сравнение с прошлым разом.
func watchDomain(domain string) domainWatch {
	var w domainWatch
	w.Records, w.CAA = records(domain)
	w.Certs, w.CertErr = ctlog.Issuances(domain)
	snap := baseline.Snapshot{}
	for _, t := range recordTypes {
		for _, host := range []string{"", "www"} {
			kind := record{Host: host, Type: t}.kind()
			var items []baseline.Item
			for _, r := range w.Records {
				if r.kind() == kind {
					items = append(items, baseline.Item{Key: r.Value})
				}
			}
			// Пустой вид запоминается только у самого домена: www без записей - норма.
			if items != nil || host == "" {
				snap[kind] = append([]baseline.Item{}, items...)
			}
		}
	}
	if w.CertErr == nil {
		snap["ct"], snap["ct-issuer"] = []baseline.Item{}, []baseline.Item{}
		for _, c := range w.Certs {
			snap["ct"] = append(snap["ct"], baseline.Item{Key: c.ID, Text: certText(c)})
			if !slices.ContainsFunc(snap["ct-issuer"], func(it baseline.Item) bool { return it.Key == c.Issuer }) {
				snap["ct-issuer"] = append(snap["ct-issuer"], baseline.Item{Key: c.Issuer})
			}
		}
	}
	baseline.Observe(domainObject(domain), snap, time.Now())
	return w
}

func certText(c ctlog.Issuance) string {
	return namesText(c.Names) + " · " + c.NotBefore.Local().Format("02.01") + " · " + c.Issuer
}

// domainNews: неотмеченное новое домена - для главного и уведомления: "сменилась запись MX, новый сертификат".
func domainNews(domain string) []string {
	var out []string
	for _, it := range baseline.Pending(domainObject(domain)) {
		var w string
		switch {
		case strings.HasPrefix(it.Kind, "dns:"):
			parts := strings.Split(it.Kind, ":")
			w = "сменилась запись " + parts[len(parts)-1]
			if it.Prev == "" {
				w = "новая запись " + parts[len(parts)-1]
			}
		case it.Kind == "ct":
			w = "новый сертификат"
		case it.Kind == "ct-issuer":
			w = "сертификат от " + it.Key
		}
		if w != "" && !slices.Contains(out, w) {
			out = append(out, w)
		}
	}
	return out
}

// recordFix: что делать, если запись сменил не ты.
func recordFix(kind string) string {
	switch t := kind[strings.LastIndex(kind, ":")+1:]; t {
	case "MX":
		return "не ты - почта уходит чужому серверу: проверь вход к регистратору и DNS-хостингу, смени пароли"
	case "NS":
		return "не ты - домен обслуживает чужой DNS: срочно к регистратору, проверь почту и пароль от него"
	case "A", "AAAA", "CNAME":
		return "не ты - сайт открывается с чужого адреса: проверь DNS-хостинг и кто туда входил"
	case "CAA":
		return "не ты - кто-то меняет, какие центры могут выпускать сертификаты"
	}
	return "не ты - выясни, кто добавил запись и зачем"
}

func sinceText(items []baseline.Item) string {
	if t := baseline.Since(items); !t.IsZero() {
		return " С " + t.Local().Format("02.01 15:04")
	}
	return ""
}

// printTwins: tim domain <домен> --twins.
func printTwins(domain string, asJSON bool) error {
	cands := twins.Candidates(domain)
	if !asJSON {
		ui.Println(ui.Dim(fmt.Sprintf("проверяю %d вариантов %s: опечатки, похожие буквы, другие зоны", len(cands), domain)))
	}
	found := twins.Registered(cands)
	if asJSON {
		return ui.JSON(found)
	}
	if len(found) == 0 {
		ui.Println(ui.Label("двойников не нашлось"))
		return nil
	}
	for _, t := range found {
		what := strings.Join(cut(t.IPs, 2), ", ")
		if t.MX {
			what += ui.Warn(" принимает почту")
		}
		ui.Println("  " + ui.Pad(t.Name, 30) + ui.Pad(t.How, 16) + what)
	}
	ui.Println(ui.Dim(fmt.Sprintf("зарегистрировано %d из %d", len(found), len(cands))))
	return nil
}
