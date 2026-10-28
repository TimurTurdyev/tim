package sites

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/cache"
	"github.com/TimurTurdyev/tim/internal/gitx"
	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/secrets"
	"github.com/TimurTurdyev/tim/internal/vulns"
)

// quietCols: колонки, которые видны, только если хоть у одного проекта есть находка; "чисто" в них не пишется.
var quietCols = []string{"git", "секреты", "зависимости"}

// SecretsCheck: секреты в git проекта. История перечитывается, только когда сдвинулась какая-нибудь ветка.
func SecretsCheck(dir string) health.Check {
	c := health.Check{Name: "секреты", Level: level.Skip}
	if !gitx.IsRepo(dir) {
		return c
	}
	refs, err := runner.Output("git", "-C", dir, "for-each-ref", "--format=%(objectname)")
	if err != nil {
		return c
	}
	h := sha256.Sum256([]byte("2 " + dir + refs)) // число - версия правил: сменились правила, кэш не годится
	found, _ := cache.Get(cache.Path(paths.State("secrets"), hex.EncodeToString(h[:8])), 30*24*time.Hour,
		func() ([]secrets.Finding, error) { return secrets.Scan(dir), nil })
	if len(found) == 0 {
		c.Level, c.Text = level.OK, "нет"
		return c
	}
	var parts []string
	for _, f := range found[:min(3, len(found))] {
		if f.Commit != "" {
			parts = append(parts, f.Where+" в истории git: коммит "+f.Commit)
		} else {
			parts = append(parts, f.What+" в "+f.Where)
		}
	}
	if n := len(found) - len(parts); n > 0 {
		parts = append(parts, fmt.Sprintf("и ещё %d", n))
	}
	c.Level, c.Text, c.Detail = level.Warn, found[0].What, strings.Join(parts, "; ")
	c.Fix = "считай пароли и ключи утёкшими и смени их; потом можно убрать файл из истории: git filter-repo --invert-paths --path " + found[0].Where
	return c
}

// DepsCheck: известные уязвимости зависимостей по lock-файлам.
func DepsCheck(dir string) health.Check {
	c := health.Check{Name: "зависимости", Level: level.Skip}
	vs, err := vulns.Check(dir)
	switch {
	case err != nil:
		c.Text, c.Detail = "не проверено", "osv.dev: "+err.Error()
		return c
	case vs == nil:
		if pkgs, _ := vulns.Lockfiles(dir); len(pkgs) > 0 {
			c.Level, c.Text = level.OK, "нет"
		}
		return c
	}
	var pkgs, parts []string
	c.Level = level.Warn
	for _, v := range vs {
		name := v.Package.Name + " " + v.Package.Version
		if slices.Contains(pkgs, name) {
			continue
		}
		pkgs = append(pkgs, name)
		if v.Severity == "CRITICAL" {
			c.Level = level.Bad
		}
		if len(parts) < 3 {
			part := name + " - " + severityWord(v.Severity)
			if v.Fixed != "" {
				part += ", исправлено в " + v.Fixed
			}
			parts = append(parts, part)
		}
	}
	if n := len(pkgs) - len(parts); n > 0 {
		parts = append(parts, fmt.Sprintf("и ещё %d", n))
	}
	c.Text, c.Detail = fmt.Sprint(len(pkgs)), strings.Join(parts, " · ")
	c.Fix = "обнови пакеты (composer update, npm update, go get); описание - osv.dev/vulnerability/" + vs[0].ID
	return c
}

func severityWord(s string) string {
	switch s {
	case "CRITICAL":
		return "критичная"
	case "HIGH":
		return "высокая"
	case "MODERATE", "MEDIUM":
		return "средняя"
	case "LOW":
		return "низкая"
	}
	return "уязвимость"
}
