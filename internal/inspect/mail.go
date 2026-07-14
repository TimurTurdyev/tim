package inspect

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/TimurTurdyev/tim/internal/level"
)

// DKIMSelectors: частые селекторы почтовых провайдеров и рассыльщиков. Селектор может быть любым,
// поэтому их отсутствие в этом списке не доказывает, что подписи нет.
var DKIMSelectors = []string{
	// общие
	"default", "dkim", "mail", "smtp", "mx", "key1", "dkim1", "dkim2",
	// Mail.ru, Яндекс, VK, российские хостинги
	"mailru", "yandex", "beget", "timeweb", "reg", "rumail",
	// Google Workspace, Microsoft 365, Zoho, Fastmail, Proton, Apple
	"google", "selector1", "selector2", "zoho", "zohomail", "fm1", "fm2", "fm3",
	"protonmail", "protonmail2", "protonmail3", "sig1",
	// рассыльщики
	"k1", "k2", "k3", "s1", "s2", "s1024", "sendgrid", "mandrill", "mailjet",
	"pm", "postmark", "hs1", "hs2", "cm", "everlytickey1", "titan",
}

// Finding: вывод проверки.
type Finding struct {
	Level level.Level
	Text  string
	// Why: короткая причина для итоговой строки, без подробностей.
	Why string
}

// AnalyzeSPF оценивает TXT-записи домена: должна быть ровно одна v=spf1.
func AnalyzeSPF(txt []string) (string, []Finding) {
	var spf []string
	for _, t := range txt {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(t)), "v=spf1") {
			spf = append(spf, t)
		}
	}
	switch len(spf) {
	case 0:
		return "", []Finding{{level.Bad, "SPF нет: почту от имени домена может слать кто угодно", "нет SPF"}}
	case 1:
	default:
		return spf[0], []Finding{{level.Bad, "SPF-записей " + strconv.Itoa(len(spf)) + ": по RFC это ошибка, проверка провалится", "несколько SPF"}}
	}
	var out []Finding
	lookups, all := 0, ""
	for _, term := range strings.Fields(strings.ToLower(spf[0]))[1:] {
		mech := strings.TrimLeft(term, "+-~?")
		name, _, _ := strings.Cut(mech, ":")
		name, _, _ = strings.Cut(name, "/")
		switch {
		case name == "include" || name == "a" || name == "mx" || name == "ptr" || name == "exists" || strings.HasPrefix(mech, "redirect="):
			lookups++
		case name == "all":
			all = term
		}
	}
	switch all {
	case "-all":
		out = append(out, Finding{level.OK, "-all: чужие серверы отклоняются", ""})
	case "~all":
		out = append(out, Finding{level.OK, "~all: чужие серверы помечаются как подозрительные", ""})
	case "+all", "all":
		out = append(out, Finding{level.Bad, "+all: разрешено слать любому серверу, SPF бесполезен", "SPF разрешает всем"})
	case "?all":
		out = append(out, Finding{level.Warn, "?all: нейтрально, чужие серверы не отсекаются", "SPF нейтрален"})
	default:
		if !strings.Contains(strings.ToLower(spf[0]), "redirect=") {
			out = append(out, Finding{level.Warn, "нет механизма all: что делать с чужими серверами, не сказано", "SPF без all"})
		}
	}
	if lookups > 10 {
		out = append(out, Finding{level.Warn, "DNS-механизмов " + strconv.Itoa(lookups) + " (больше 10): часть получателей отбросит SPF", "SPF больше 10 запросов"})
	}
	return spf[0], out
}

// AnalyzeDMARC оценивает TXT-записи _dmarc.<домен>.
func AnalyzeDMARC(txt []string) (string, []Finding) {
	rec := ""
	for _, t := range txt {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(t)), "v=dmarc1") {
			rec = t
		}
	}
	if rec == "" {
		return "", []Finding{{level.Bad, "DMARC нет: получатели не знают, что делать с подделкой", "нет DMARC"}}
	}
	tags := map[string]string{}
	for _, part := range strings.Split(rec, ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok {
			tags[strings.ToLower(k)] = strings.TrimSpace(v)
		}
	}
	var out []Finding
	switch strings.ToLower(tags["p"]) {
	case "reject":
		out = append(out, Finding{level.OK, "p=reject: подделки отклоняются", ""})
	case "quarantine":
		out = append(out, Finding{level.OK, "p=quarantine: подделки уходят в спам", ""})
	case "none":
		out = append(out, Finding{level.Warn, "p=none: только наблюдение, подделки доходят", "DMARC только наблюдает"})
	default:
		out = append(out, Finding{level.Bad, "нет политики p=", "DMARC без политики"})
	}
	if pct := tags["pct"]; pct != "" && pct != "100" {
		out = append(out, Finding{level.Warn, "pct=" + pct + ": политика применяется не ко всем письмам", "DMARC не для всех писем"})
	}
	if tags["rua"] == "" {
		out = append(out, Finding{level.Info, "нет rua: отчёты о подделках никуда не приходят", ""})
	}
	return rec, out
}

// Mail: записи почты домена.
type Mail struct {
	// NoAnswer: DNS не ответил вовремя - про почту ничего не известно, это не "нет MX".
	NoAnswer bool
	NullMX   bool
	MX       []string
	SPF      string
	DMARC    string
	DKIM     map[string]string
	SPFF     []Finding
	DMRF     []Finding
}

func lookupTXT(ctx context.Context, name string) []string {
	txt, _ := net.DefaultResolver.LookupTXT(ctx, name)
	return txt
}

func MailCheck(domain string, selectors []string) *Mail {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	m := &Mail{DKIM: map[string]string{}}
	var wg sync.WaitGroup
	var mu sync.Mutex
	wg.Go(func() {
		mx, err := net.DefaultResolver.LookupMX(ctx, domain)
		if dnsErr, ok := errors.AsType[*net.DNSError](err); err != nil && (!ok || !dnsErr.IsNotFound) {
			m.NoAnswer = true
		}
		for _, r := range mx {
			h := strings.TrimSuffix(r.Host, ".")
			if h == "" {
				m.NullMX = true
				continue
			}
			m.MX = append(m.MX, h+" "+strconv.Itoa(int(r.Pref)))
		}
	})
	wg.Go(func() { m.SPF, m.SPFF = AnalyzeSPF(lookupTXT(ctx, domain)) })
	wg.Go(func() { m.DMARC, m.DMRF = AnalyzeDMARC(lookupTXT(ctx, "_dmarc."+domain)) })
	for _, sel := range selectors {
		wg.Go(func() {
			for _, t := range lookupTXT(ctx, sel+"._domainkey."+domain) {
				if strings.Contains(t, "p=") {
					mu.Lock()
					m.DKIM[sel] = t
					mu.Unlock()
					return
				}
			}
		})
	}
	wg.Wait()
	return m
}

// Verdict: общая оценка почты домена и короткие причины. ok - принимает и защищена от подделок;
// warn - работает, но есть чего не хватает; bad - есть серьёзная дыра или почты нет; skip - DNS не ответил.
func (m *Mail) Verdict() (verdict level.Level, reasons []string) {
	if m.NoAnswer {
		return level.Skip, []string{"DNS не ответил"}
	}
	verdict = level.OK
	drop := func(l level.Level) { verdict = max(verdict, l) }
	seen := map[string]bool{}
	add := func(why string) {
		if why != "" && !seen[why] {
			seen[why] = true
			reasons = append(reasons, why)
		}
	}
	for _, f := range append(append([]Finding{}, m.SPFF...), m.DMRF...) {
		if f.Level >= level.Warn {
			drop(f.Level)
			add(f.Why)
		}
	}
	if len(m.DKIM) == 0 {
		// Селектор DKIM может быть любым: отсутствие на частых не доказывает, что подписи нет.
		add("DKIM на частых селекторах не найден")
		drop(level.Warn)
	}
	switch {
	case m.NullMX:
		// Домен явно сказал, что почту не принимает: это осознанная настройка, а не дыра.
		if m.SPF != "" && m.DMARC != "" {
			return level.OK, nil
		}
		drop(level.Warn)
	case len(m.MX) == 0:
		add("нет MX")
		drop(level.Bad)
	}
	return verdict, reasons
}
