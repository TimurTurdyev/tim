package inspect

import (
	"context"
	"net"
	"strconv"
	"strings"
	"sync"
)

// Finding: вывод проверки, Level: ok, warn, bad, info.
type Finding struct {
	Level string
	Text  string
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
		return "", []Finding{{"bad", "SPF нет: почту от имени домена может слать кто угодно"}}
	case 1:
	default:
		return spf[0], []Finding{{"bad", "SPF-записей " + strconv.Itoa(len(spf)) + ": по RFC это ошибка, проверка провалится"}}
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
		out = append(out, Finding{"ok", "-all: чужие серверы отклоняются"})
	case "~all":
		out = append(out, Finding{"ok", "~all: чужие серверы помечаются как подозрительные"})
	case "+all", "all":
		out = append(out, Finding{"bad", "+all: разрешено слать любому серверу, SPF бесполезен"})
	case "?all":
		out = append(out, Finding{"warn", "?all: нейтрально, чужие серверы не отсекаются"})
	default:
		if !strings.Contains(strings.ToLower(spf[0]), "redirect=") {
			out = append(out, Finding{"warn", "нет механизма all: что делать с чужими серверами, не сказано"})
		}
	}
	if lookups > 10 {
		out = append(out, Finding{"warn", "DNS-механизмов " + strconv.Itoa(lookups) + " (больше 10): часть получателей отбросит SPF"})
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
		return "", []Finding{{"bad", "DMARC нет: получатели не знают, что делать с подделкой"}}
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
		out = append(out, Finding{"ok", "p=reject: подделки отклоняются"})
	case "quarantine":
		out = append(out, Finding{"ok", "p=quarantine: подделки уходят в спам"})
	case "none":
		out = append(out, Finding{"warn", "p=none: только наблюдение, подделки доходят"})
	default:
		out = append(out, Finding{"bad", "нет политики p="})
	}
	if pct := tags["pct"]; pct != "" && pct != "100" {
		out = append(out, Finding{"warn", "pct=" + pct + ": политика применяется не ко всем письмам"})
	}
	if tags["rua"] == "" {
		out = append(out, Finding{"info", "нет rua: отчёты о подделках никуда не приходят"})
	}
	return rec, out
}

var DKIMSelectors = []string{"default", "google", "selector1", "selector2", "mail", "dkim", "k1", "s1", "s2", "yandex", "mx", "smtp"}

// Mail: записи почты домена.
type Mail struct {
	NullMX bool
	MX     []string
	SPF    string
	DMARC  string
	DKIM   map[string]string
	SPFF   []Finding
	DMRF   []Finding
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
	wg.Add(1)
	go func() {
		defer wg.Done()
		mx, _ := net.DefaultResolver.LookupMX(ctx, domain)
		for _, r := range mx {
			h := strings.TrimSuffix(r.Host, ".")
			if h == "" {
				m.NullMX = true
				continue
			}
			m.MX = append(m.MX, h+" "+strconv.Itoa(int(r.Pref)))
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		m.SPF, m.SPFF = AnalyzeSPF(lookupTXT(ctx, domain))
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		m.DMARC, m.DMRF = AnalyzeDMARC(lookupTXT(ctx, "_dmarc."+domain))
	}()
	for _, sel := range selectors {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, t := range lookupTXT(ctx, sel+"._domainkey."+domain) {
				if strings.Contains(t, "p=") {
					mu.Lock()
					m.DKIM[sel] = t
					mu.Unlock()
					return
				}
			}
		}()
	}
	wg.Wait()
	return m
}

// Verdict: общая оценка почты домена. ok - принимает и защищена от подделок;
// warn - работает, но есть чего не хватает; bad - есть серьёзная дыра или почты нет.
func (m *Mail) Verdict() (level string, missing []string) {
	level = "ok"
	drop := func(l string) {
		if l == "bad" || level == "ok" && l == "warn" {
			level = l
		}
	}
	for _, f := range append(append([]Finding{}, m.SPFF...), m.DMRF...) {
		if f.Level == "bad" || f.Level == "warn" {
			drop(f.Level)
		}
	}
	if m.SPF == "" {
		missing = append(missing, "SPF")
	}
	if m.DMARC == "" {
		missing = append(missing, "DMARC")
	}
	if len(m.DKIM) == 0 {
		// Селектор DKIM может быть любым: отсутствие на частых не доказывает, что подписи нет.
		missing = append(missing, "DKIM на частых селекторах")
		drop("warn")
	}
	switch {
	case m.NullMX:
		// Домен явно сказал, что почту не принимает: это осознанная настройка, а не дыра.
		if m.SPF != "" && m.DMARC != "" {
			return "ok", nil
		}
		drop("warn")
	case len(m.MX) == 0:
		missing = append(missing, "MX")
		drop("bad")
	}
	return level, missing
}
