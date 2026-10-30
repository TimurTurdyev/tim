package sites

import (
	"regexp"
	"slices"
	"strings"

	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Что сделать с почтой домена: у каждой беды - действие словами и, где можно, готовая запись DNS. Сервис
// почты узнаётся по MX: у Яндекса, Mail.ru и Google свои include для SPF и свои места, где включается DKIM.

// providers: почтовые сервисы по MX.
var providers = []mailProvider{
	{"Яндекс 360", []string{"yandex"}, "include:_spf.yandex.net", "admin.yandex.ru -> Почта -> DKIM-подписи, селектор mail"},
	{"Mail.ru для бизнеса", []string{"mail.ru"}, "include:_spf.mail.ru", "biz.mail.ru -> домен -> DKIM, селектор mailru"},
	{"Google Workspace", []string{"google.com", "googlemail.com"}, "include:_spf.google.com", "admin.google.com -> Gmail -> Аутентификация почты, селектор google"},
	{"Beget", []string{"beget"}, "include:beget.com", "панель Beget -> Почта -> DKIM"},
	{"Timeweb", []string{"timeweb"}, "include:_spf.timeweb.ru", "панель Timeweb -> Почта -> DKIM"},
}

var dmarcP = regexp.MustCompile(`(?i)\bp\s*=\s*none`)

// mailProvider: сервис почты и как у него настраивается подпись.
type mailProvider struct {
	name  string
	mx    []string
	spf   string // механизм для SPF
	dkimS string // где включить DKIM
}

// mailFix: что сделать с частью почты (MX, SPF, DKIM, DMARC).
type mailFix struct {
	Part   string
	Level  level.Level
	Do     string
	Record string // запись целиком: "_dmarc.example.com TXT "v=DMARC1; ...""
}

func providerOf(mx []string) (mailProvider, bool) {
	for _, p := range providers {
		for _, m := range mx {
			if slices.ContainsFunc(p.mx, func(s string) bool { return strings.Contains(strings.ToLower(m), s) }) {
				return p, true
			}
		}
	}
	return mailProvider{}, false
}

// noMail: на домене нет ничего из почты - нужна инструкция целиком, а не четыре "нет".
func noMail(m *inspect.Mail) bool {
	return !m.NoAnswer && !m.NullMX && len(m.MX) == 0 && m.SPF == "" && m.DMARC == "" && len(m.DKIM) == 0
}

func mailFixes(domain string, m *inspect.Mail) []mailFix {
	if m.NoAnswer {
		return nil
	}
	var out []mailFix
	add := func(part string, l level.Level, do, record string) {
		out = append(out, mailFix{part, l, do, record})
	}
	prov, known := providerOf(m.MX)
	if len(m.MX) == 0 && !m.NullMX {
		add("MX", level.Bad, "почта нужна - подключи домен в почтовом сервисе и добавь MX, который он даст; не нужна - "+
			"закрой домен от подделки: MX 0 ., SPF -all, DMARC p=reject", "")
	}
	// SPF: чем слать - include сервиса, а у своего сервера - a и mx.
	sender := "a mx"
	if known {
		sender = prov.spf
	}
	for _, f := range m.SPFF {
		switch {
		case f.Why == "нет SPF" && !m.NullMX:
			add("SPF", level.Bad, "перечисли, кто шлёт письма от домена", domain+` TXT "v=spf1 `+sender+` ~all"`)
		case f.Why == "несколько SPF":
			add("SPF", level.Bad, "оставь одну запись v=spf1: объедини механизмы в неё, лишние удали", "")
		case f.Why == "SPF разрешает всем" || f.Why == "SPF нейтрален" || f.Why == "SPF без all":
			add("SPF", f.Level, "закончи запись на ~all: чужие серверы будут помечаться", domain+` TXT "`+withAll(m.SPF)+`"`)
		case f.Why == "SPF больше 10 запросов":
			add("SPF", level.Warn, "убери лишние include или замени их адресами ip4: - больше 10 запросов DNS получатели не делают", "")
		}
	}
	if len(m.DKIM) == 0 && !m.NullMX {
		do := "включи подпись у того, кто шлёт письма, он даст запись <селектор>._domainkey; свой селектор проверит tim mail " + domain + " --selector <имя>"
		if known {
			do = "включи DKIM: " + prov.dkimS
		} else if len(m.MX) > 0 {
			do = "на своём сервере - opendkim или rspamd, открытый ключ публикуется TXT <селектор>._domainkey." + domain
		}
		add("DKIM", level.Warn, do, "")
	}
	rua := "rua=mailto:dmarc@" + domain
	for _, f := range m.DMRF {
		switch {
		case f.Why == "нет DMARC" && !m.NullMX:
			add("DMARC", level.Bad, "начни с наблюдения: p=none ничего не ломает, отчёты придут на почту; через 2-4 недели - p=quarantine",
				"_dmarc."+domain+` TXT "v=DMARC1; p=none; `+rua+`"`)
		case f.Why == "DMARC только наблюдает":
			do := "через 2-4 недели, если в отчётах только свои серверы, - p=quarantine: подделки в спам"
			if r := dmarcTag(m.DMARC, "rua"); r != "" {
				do = "через 2-4 недели, если в отчётах на " + strings.TrimPrefix(r, "mailto:") + " только свои серверы, - p=quarantine: подделки в спам"
			}
			add("DMARC", level.Warn, do, "_dmarc."+domain+` TXT "`+dmarcP.ReplaceAllString(m.DMARC, "p=quarantine")+`"`)
		case f.Why == "DMARC без политики":
			add("DMARC", level.Bad, "добавь политику, для начала p=none", "_dmarc."+domain+` TXT "v=DMARC1; p=none; `+rua+`"`)
		case f.Why == "DMARC не для всех писем":
			add("DMARC", level.Warn, "убери pct=, чтобы политика касалась всех писем", "")
		case strings.HasPrefix(f.Text, "нет rua"):
			add("DMARC", level.Info, "добавь адрес для отчётов - по ним видно, кто шлёт от имени домена",
				"_dmarc."+domain+` TXT "`+strings.TrimSuffix(strings.TrimSpace(m.DMARC), ";")+"; "+rua+`"`)
		}
	}
	return out
}

// withAll: запись SPF с ~all вместо +all, ?all или без all.
func withAll(spf string) string {
	var parts []string
	for _, t := range strings.Fields(spf) {
		if strings.TrimLeft(strings.ToLower(t), "+-~?") != "all" {
			parts = append(parts, t)
		}
	}
	return strings.Join(append(parts, "~all"), " ")
}

func dmarcTag(rec, tag string) string {
	for _, part := range strings.Split(rec, ";") {
		if k, v, ok := strings.Cut(strings.TrimSpace(part), "="); ok && strings.EqualFold(k, tag) {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// printMailGuide: почты на домене нет - два пути целиком.
func printMailGuide(domain string) {
	ui.Println(ui.Info("Почты на домене нет. Два пути"))
	ui.Println("")
	ui.Println(ui.Label("1. Почта нужна") + ui.Dim(" (info@"+domain+" и письма с сайта)"))
	for _, l := range []string{
		"подключи домен в почтовом сервисе: Яндекс 360, Mail.ru для бизнеса, Google Workspace или свой сервер;",
		"сервис попросит подтвердить домен записью TXT и даст MX - добавь их в DNS;",
		"SPF: " + domain + ` TXT "v=spf1 include:<сервиса> ~all" (у Яндекса - include:_spf.yandex.net);`,
		"DKIM: включи в панели сервиса, он даст запись <селектор>._domainkey;",
		"DMARC: _dmarc." + domain + ` TXT "v=DMARC1; p=none; rua=mailto:dmarc@` + domain + `", через 2-4 недели - p=quarantine;`,
		"проверить: tim mail " + domain,
	} {
		ui.Println("   " + l)
	}
	ui.Println("")
	ui.Println(ui.Label("2. Почта не нужна") + ui.Dim(" - закрой домен от подделки, три записи:"))
	for _, l := range []string{domain + "  MX  0 .", domain + `  TXT  "v=spf1 -all"`, "_dmarc." + domain + `  TXT  "v=DMARC1; p=reject"`} {
		ui.Println("   " + l)
	}
}

// printMailFixes: что сделать - по делу на строку, готовые записи под ним.
func printMailFixes(fixes []mailFix) {
	if len(fixes) == 0 {
		return
	}
	ui.Println()
	ui.Println(ui.Info("Что сделать"))
	for _, f := range fixes {
		ui.Println("  " + ui.Pad(f.Part, 7) + f.Do)
		if f.Record != "" {
			ui.Println("  " + ui.Pad("", 7) + f.Record)
		}
	}
}
