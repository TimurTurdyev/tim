package audit

import (
	"net/url"
	"slices"
	"strings"

	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/par"
)

const scriptsToScan = 3

var (
	// loginMarks: след входа через внешний сервис в HTML или скрипте -> сервис. Пиксель Facebook
	// (connect.facebook.net/.../fbevents.js) - не вход, поэтому у Facebook только кнопка и окно входа.
	loginMarks = []struct{ mark, name string }{
		{"accounts.google.com/gsi", "Google"}, {"accounts.google.com/o/oauth2", "Google"}, {"g_id_onload", "Google"},
		{"appleid.apple.com", "Apple ID"}, {"appleid.cdn-apple.com", "Apple ID"},
		{"facebook.com/dialog/oauth", "Facebook"}, {"fb-login-button", "Facebook"}, {"fb:login-button", "Facebook"},
		{"identitytoolkit.googleapis.com", "Firebase"}, {"firebase-auth", "Firebase"}, {"firebaseapp.com/__/auth", "Firebase"},
		{".auth0.com", "Auth0"}, {"github.com/login/oauth", "GitHub"}, {"login.microsoftonline.com", "Microsoft"},
		{"telegram-widget.js", "Telegram"}, {"ulogin.ru", "uLogin"},
	}
	// loginPaths: адрес страницы входа среди ссылок главной.
	loginPaths = []string{"account/login", "/login", "signin", "sign-in", "/auth", "/vhod"}
)

// ForeignLogins: какие внешние сервисы входа видны в тексте страницы или скрипта, без повторов.
func ForeignLogins(text string) []string {
	low := strings.ToLower(text)
	var out []string
	for _, m := range loginMarks {
		if strings.Contains(low, m.mark) && !slices.Contains(out, m.name) {
			out = append(out, m.name)
		}
	}
	return out
}

// LoginFindings: решение по найденным сервисам. Запрет входа через иностранные сервисы (406-ФЗ)
// касается российских сайтов; Telegram в нём спорный, uLogin - посредник, смотреть его список.
func LoginFindings(found []string, russian bool) []Finding {
	var foreign []string
	var out []Finding
	for _, name := range found {
		switch name {
		case "Telegram":
			if russian {
				out = append(out, Finding{Area: "вход", Level: level.Info, Text: "вход через Telegram",
					Why: "допустим ли он по 406-ФЗ, пока спорно: прямого запрета нет, разъяснений тоже"})
			}
		case "uLogin":
			if russian {
				out = append(out, Finding{Area: "вход", Level: level.Warn, Text: "вход через виджет uLogin",
					Why: "uLogin может пускать через Google, Facebook и другие иностранные сервисы - за них штраф по ст. 13.55 КоАП",
					Fix: "оставить в виджете только VK ID, Яндекс ID, Сбер ID, T-ID или Госуслуги"})
			}
		default:
			foreign = append(foreign, name)
		}
	}
	if len(foreign) > 0 && russian {
		out = append(out, Finding{Area: "вход", Level: level.Bad, Text: "вход через " + strings.Join(foreign, ", "),
			Why: "российским сайтам нельзя пускать по иностранным аккаунтам (406-ФЗ), с 07.07.2026 штраф по ст. 13.55 КоАП: юрлицам 500-700 тыс. руб., ИП 30-50 тыс.",
			Fix: "свой вход по почте или телефону, из внешних - VK ID, Яндекс ID, Сбер ID, T-ID или Госуслуги"})
	}
	return out
}

// Russian: сайт в российской зоне или страница на русском - для него действует 406-ФЗ.
func Russian(host, lang string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, zone := range []string{".ru", ".su", ".xn--p1ai", ".рф"} {
		if strings.HasSuffix(host, zone) {
			return true
		}
	}
	return strings.HasPrefix(strings.ToLower(lang), "ru")
}

// login: главная, страница входа и до трёх своих скриптов - вход часто живёт только в них.
func login(s *site) []Finding {
	texts := []string{s.home.body}
	pages := []string{loginPage(s)}
	for _, a := range s.page.Assets {
		if len(pages) > scriptsToScan {
			break
		}
		if u, err := url.Parse(a); err == nil && strings.HasSuffix(u.Path, ".js") {
			pages = append(pages, a)
		}
	}
	bodies := par.Map(pages, len(pages), func(u string) string {
		if u == "" {
			return ""
		}
		return s.get(u, true).body
	})
	var found []string
	for _, t := range append(texts, bodies...) {
		for _, name := range ForeignLogins(t) {
			if !slices.Contains(found, name) {
				found = append(found, name)
			}
		}
	}
	return LoginFindings(found, Russian(s.host, s.page.Lang))
}

// loginPage: ссылка на вход с главной; у OpenCart без такой ссылки - его стандартный адрес.
func loginPage(s *site) string {
	for _, l := range s.page.Links {
		low := strings.ToLower(l)
		if slices.ContainsFunc(loginPaths, func(p string) bool { return strings.Contains(low, p) }) {
			return l
		}
	}
	if strings.Contains(strings.ToLower(s.home.body), "catalog/view/") {
		return s.base + "/index.php?route=account/login"
	}
	return ""
}
