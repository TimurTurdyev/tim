package health

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/TimurTurdyev/tim/internal/gitx"
	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Пороги обхода. Собраны здесь, а не разбросаны по проверкам, чтобы их было видно и можно было проверить.
const (
	CertWarnDays   = 21
	CertBadDays    = 7
	DomainWarnDays = 30
	DomainBadDays  = 10
)

// EnvGit: что git знает про .env проекта.
type EnvGit struct {
	Exists  bool // файл есть в папке
	Tracked bool // лежит в репозитории
	Ignored bool // попадает под .gitignore
}

// GitCheck: незакоммиченное - обычное состояние работы, а неотправленное живёт только на этой машине.
// .env в репозитории - утечка секретов, хуже всего остального в колонке.
func GitCheck(g gitx.State, env EnvGit) Check {
	c := Check{Name: "git", Level: OK}
	var parts []string
	if g.Changes > 0 {
		parts = append(parts, strconv.Itoa(g.Changes)+" изм")
	}
	if g.Ahead > 0 {
		parts = append(parts, strconv.Itoa(g.Ahead)+" не отпр")
		c.Level = Warn
	}
	if g.Behind > 0 {
		parts = append(parts, strconv.Itoa(g.Behind)+" позади")
		c.Level = Warn
	}
	switch {
	case env.Tracked:
		parts = append([]string{".env в git"}, parts...)
		c.Level = Bad
		c.Detail = ".env лежит в репозитории: секреты уже в истории, их надо сменить, а файл убрать (git rm --cached .env)"
	case env.Exists && !env.Ignored:
		parts = append([]string{".env не в ignore"}, parts...)
		if c.Level < Warn {
			c.Level = Warn
		}
		c.Detail = ".env не в .gitignore: один git add . - и секреты в репозитории"
	}
	if len(parts) == 0 {
		c.Text = "чисто"
		return c
	}
	c.Text = strings.Join(parts, ", ")
	return c
}

// SiteCheck переводит ответ сайта в уровень. Ошибка запроса хуже любого кода.
func SiteCheck(status int, err error) Check {
	c := Check{Name: "сайт", Level: OK}
	if err != nil {
		c.Level, c.Text, c.Detail = Bad, "нет ответа", "сайт не отвечает: "+ui.ShortErr(err)
		return c
	}
	c.Text = strconv.Itoa(status)
	switch {
	case status >= 500:
		c.Level, c.Detail = Bad, fmt.Sprintf("сайт отвечает %d", status)
	case status >= 400:
		c.Level, c.Detail = Warn, fmt.Sprintf("сайт отвечает %d", status)
	}
	return c
}

// MailCheck переводит оценку почты домена в уровень.
func MailCheck(level string, reasons []string) Check {
	c := Check{Name: "почта"}
	switch level {
	case "bad":
		c.Level, c.Text = Bad, "плохо"
	case "warn":
		c.Level, c.Text = Warn, "неполно"
	default:
		c.Level, c.Text = OK, "ок"
	}
	if c.Level >= Warn && len(reasons) > 0 {
		c.Detail = "почта: " + strings.Join(reasons, "; ")
	}
	return c
}

// Site запрашивает сайт и оценивает ответ.
func Site(url string) Check {
	page, err := inspect.HTTP(url)
	if err != nil {
		return SiteCheck(0, err)
	}
	return SiteCheck(page.Status, nil)
}

// Cert проверяет срок и пригодность сертификата.
func Cert(host string) Check {
	info, err := inspect.Cert(host, "443")
	if err != nil {
		return Check{Name: "сертификат", Level: Bad, Text: "нет", Detail: "сертификат не получен: " + ui.ShortErr(err)}
	}
	c := Deadline("сертификат", info.Leaf().NotAfter, CertWarnDays, CertBadDays)
	if info.VerifyErr != nil {
		c.Level = Bad
		c.Detail = "сертификат не проходит проверку: " + ui.ShortErr(info.VerifyErr)
	}
	return c
}

// Mail проверяет записи почты домена.
func Mail(domain string) Check {
	return MailCheck(inspect.MailCheck(domain, inspect.DKIMSelectors).Verdict())
}

// DomainReg проверяет, до каких пор оплачен домен.
func DomainReg(domain string) Check {
	d, err := Registration(domain)
	if err != nil || d.Expires.IsZero() {
		return Check{Name: "домен", Level: Skip, Text: "-"}
	}
	c := Deadline("регистрация домена", d.Expires, DomainWarnDays, DomainBadDays)
	c.Name = "домен"
	return c
}
