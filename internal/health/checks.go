package health

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Пороги обхода. Собраны здесь, а не разбросаны по проверкам, чтобы их было видно и можно было проверить.
const (
	CertWarnDays   = 21
	CertBadDays    = 7
	DomainWarnDays = 30
	DomainBadDays  = 10
)

// SiteCheck переводит ответ сайта в уровень. Ошибка запроса хуже любого кода.
func SiteCheck(status int, err error) Check {
	c := Check{Name: "сайт", Level: level.OK}
	if err != nil {
		c.Level, c.Text, c.Detail = level.Bad, "нет ответа", "сайт не отвечает: "+ui.ShortErr(err)
		return c
	}
	c.Text = strconv.Itoa(status)
	switch {
	case status >= 500:
		c.Level, c.Detail = level.Bad, fmt.Sprintf("сайт отвечает %d", status)
	case status >= 400:
		c.Level, c.Detail = level.Warn, fmt.Sprintf("сайт отвечает %d", status)
	}
	return c
}

// MailCheck переводит оценку почты домена в уровень.
func MailCheck(verdict level.Level, reasons []string) Check {
	c := Check{Name: "почта", Level: verdict}
	switch verdict {
	case level.Skip:
		c.Text = "-"
	case level.Bad:
		c.Text = "плохо"
	case level.Warn:
		c.Text = "неполно"
	default:
		c.Level, c.Text = level.OK, "ок"
	}
	if c.Level >= level.Warn && len(reasons) > 0 {
		c.Detail = "почта: " + strings.Join(reasons, "; ")
	}
	return c
}

// Site запрашивает сайт и оценивает ответ.
func Site(url string) Check {
	page, err := inspect.HTTP(url)
	if err != nil {
		c := SiteCheck(0, err)
		c.Fix = "на каком шаге рвётся - DNS, соединение, TLS: tim http " + url
		return c
	}
	c := SiteCheck(page.Status, nil)
	switch {
	case page.Status >= 500:
		c.Fix = "ошибка на сервере сайта: журнал - tim server <алиас> --run errors"
	case page.Status >= 400:
		c.Fix = "куда ведёт и что отвечает: tim http " + url + " -L"
	}
	return c
}

// Cert проверяет срок и пригодность сертификата.
func Cert(host string) Check {
	info, err := inspect.Cert(host, "443")
	if err != nil {
		return Check{Name: "сертификат", Level: level.Bad, Text: "нет", Detail: "сертификат не получен: " + ui.ShortErr(err),
			Fix: "что не так - tim cert " + host}
	}
	c := Deadline("сертификат", info.Leaf().NotAfter, CertWarnDays, CertBadDays)
	if c.Level >= level.Warn {
		c.Fix = "продлить: certbot renew на сервере или в панели хостинга; Let's Encrypt продлевает сам, если продление настроено"
	}
	if info.VerifyErr != nil {
		c.Level = level.Bad
		c.Detail = "сертификат не проходит проверку: " + ui.ShortErr(info.VerifyErr)
		c.Fix = "цепочка неполная или сертификат на другое имя: tim cert " + host
	}
	return c
}

// Mail проверяет записи почты домена.
func Mail(domain string) Check {
	c := MailCheck(inspect.MailCheck(domain, inspect.DKIMSelectors).Verdict())
	if c.Level >= level.Warn {
		c.Fix = "что добавить в DNS - tim mail " + domain
	}
	return c
}

// DomainReg проверяет, до каких пор оплачен домен.
func DomainReg(domain string) Check {
	d, err := Registration(domain)
	if err != nil || d.Expires.IsZero() {
		return Check{Name: "домен", Level: level.Skip, Text: "-"}
	}
	c := Deadline("регистрация домена", d.Expires, DomainWarnDays, DomainBadDays)
	c.Name = "домен"
	if c.Level >= level.Warn {
		c.Fix = "продлить у регистратора"
		if d.Registrar != "" {
			c.Fix += " " + d.Registrar
		}
		c.Fix += "; включи автопродление, если оно есть"
	}
	return c
}
