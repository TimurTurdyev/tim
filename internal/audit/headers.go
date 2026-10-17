package audit

import (
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/TimurTurdyev/tim/internal/level"
)

// hstsMinAge: полгода - короче браузеры и preload-список считают HSTS ненадёжным.
const hstsMinAge = 15552000

var maxAgeRe = regexp.MustCompile(`(?i)max-age\s*=\s*"?(\d+)`)

// sessionCookies: окончания имён кук сессии - их кража равна входу под пользователем.
var sessionCookies = []string{"_session", "phpsessid", "sessionid", "connect.sid"}

// Headers: заголовки безопасности главной. https - главная отдалась по https: без него HSTS не работает.
// Referrer-Policy не проверяется: браузеры с 2021 года по умолчанию не отдают чужим полный адрес.
func Headers(h http.Header, https bool) []Finding {
	var out []Finding
	add := func(l level.Level, text, why, fix string) {
		out = append(out, Finding{Area: "заголовки", Level: l, Text: text, Why: why, Fix: fix})
	}
	if hsts := h.Get("Strict-Transport-Security"); https && hsts == "" {
		add(level.Warn, "нет HSTS", "браузер может открыть сайт по http, и первый запрос перехватят в чужом Wi-Fi",
			"Strict-Transport-Security: max-age=31536000; includeSubDomains")
	} else if https && maxAge(hsts) < hstsMinAge {
		add(level.Info, "HSTS короче полугода", "после короткого срока браузер снова пробует http",
			"max-age=31536000")
	}
	csp := h.Get("Content-Security-Policy")
	switch {
	case csp == "":
		add(level.Warn, "нет CSP", "внедрённый скрипт (XSS) выполнится без помех: браузеру не сказано, откуда можно грузить скрипты",
			"Content-Security-Policy, начать с default-src 'self' и проверить в режиме Content-Security-Policy-Report-Only")
	case inlineScripts(csp):
		add(level.Info, "CSP разрешает inline-скрипты", "'unsafe-inline' без nonce почти обнуляет защиту от XSS",
			"nonce или hash для своих inline-скриптов вместо 'unsafe-inline'")
	}
	if h.Get("X-Frame-Options") == "" && !strings.Contains(csp, "frame-ancestors") {
		add(level.Warn, "сайт можно встроить во фрейм", "кликджекинг: чужая страница накроет кнопки сайта прозрачным фреймом",
			"Content-Security-Policy: frame-ancestors 'self' или X-Frame-Options: SAMEORIGIN")
	}
	if !strings.EqualFold(strings.TrimSpace(h.Get("X-Content-Type-Options")), "nosniff") {
		add(level.Info, "нет X-Content-Type-Options", "браузер угадывает тип файла и может выполнить загруженную картинку как скрипт",
			"X-Content-Type-Options: nosniff")
	}
	return out
}

// Cookies: флаги кук главной. Одна находка на вид беды, имена кук списком.
func Cookies(setCookie []string, https bool) []Finding {
	var noSecure, open []string
	for _, line := range setCookie {
		c, err := http.ParseSetCookie(line)
		if err != nil {
			continue
		}
		if https && !c.Secure && !slices.Contains(noSecure, c.Name) {
			noSecure = append(noSecure, c.Name)
		}
		if session(c.Name) && !c.HttpOnly && !slices.Contains(open, c.Name) {
			open = append(open, c.Name)
		}
	}
	var out []Finding
	if len(open) > 0 {
		out = append(out, Finding{Area: "куки", Level: level.Bad, Text: "кука сессии доступна скриптам: " + strings.Join(open, ", "),
			Why: "любой внедрённый скрипт уносит сессию и входит под пользователем",
			Fix: "HttpOnly у куки сессии; в Laravel - 'http_only' => true в config/session.php"})
	}
	if len(noSecure) > 0 {
		out = append(out, Finding{Area: "куки", Level: level.Warn, Text: "куки без Secure: " + strings.Join(noSecure, ", "),
			Why: "браузер отправит их и по http, где их видно в открытой сети",
			Fix: "Secure у всех кук сайта на https; в Laravel - SESSION_SECURE_COOKIE=true"})
	}
	return out
}

func maxAge(hsts string) int {
	m := maxAgeRe.FindStringSubmatch(hsts)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// inlineScripts: скриптам разрешён inline без nonce и hash - с ними браузеры 'unsafe-inline' игнорируют.
func inlineScripts(csp string) bool {
	rules := map[string]string{}
	for _, d := range strings.Split(csp, ";") {
		name, value, _ := strings.Cut(strings.TrimSpace(d), " ")
		rules[strings.ToLower(name)] = value
	}
	src, ok := rules["script-src"]
	if !ok {
		src = rules["default-src"]
	}
	return strings.Contains(src, "'unsafe-inline'") && !strings.Contains(src, "'nonce-") && !strings.Contains(src, "'sha")
}

func session(name string) bool {
	n := strings.ToLower(name)
	for _, s := range sessionCookies {
		if n == strings.TrimPrefix(s, "_") || strings.HasSuffix(n, s) {
			return true
		}
	}
	return false
}
