package audit

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/level"
)

// ocAdminVersionRe: подвал входа в админку OpenCart по умолчанию - "OpenCart © 2009-2024 ... Version 3.0.3.8".
var ocAdminVersionRe = regexp.MustCompile(`(?is)opencart.{0,400}?version\s*(\d+\.\d+(?:\.\d+){0,2})`)

// OpenCartBranch: ветка OpenCart по главной. Свои темы меняют всё, поэтому только признаки стандартной
// шапки: у четвёртой Bootstrap в catalog/view/stylesheet и параметр language в адресах; jQuery 1.7.1 - 1.5;
// jQuery 2.1.1 - вторая или ранняя третья, их снаружи не различить; jQuery 3 при старом пути Bootstrap -
// поздняя третья (3.0.4). Пусто - не понять.
func OpenCartBranch(html string) string {
	low := strings.ToLower(html)
	oldBootstrap := strings.Contains(low, "catalog/view/javascript/bootstrap/css/bootstrap.min.css")
	switch {
	case strings.Contains(low, "catalog/view/stylesheet/bootstrap.css") ||
		strings.Contains(low, "index.php?route=") && strings.Contains(low, "&amp;language="):
		return "4"
	case strings.Contains(low, "catalog/view/javascript/jquery/jquery-1.7.1"):
		return "1.5"
	case strings.Contains(low, "catalog/view/javascript/jquery/jquery-2.1.1"):
		return "2-3"
	case strings.Contains(low, "catalog/view/javascript/jquery/jquery-3") && oldBootstrap:
		return "3"
	}
	return ""
}

// OpenCartVersion: точная версия из страницы входа в админку, если подвал не убрали.
func OpenCartVersion(adminHTML string) string {
	if m := ocAdminVersionRe.FindStringSubmatch(adminHTML); m != nil {
		return m[1]
	}
	return ""
}

// OpenCartFindings: ветки 1.5 и 2 не обновляются с 2014 и 2016 годов и не идут на PHP 8, а значит,
// сайт держится на PHP без обновлений безопасности. version - из админки, точнее ветки с главной.
func OpenCartFindings(branch, version string) []Finding {
	if version != "" {
		branch = strings.SplitN(version, ".", 2)[0]
		if strings.HasPrefix(version, "1.5") {
			branch = "1.5"
		}
	}
	name := "OpenCart " + version
	if version == "" {
		name = "OpenCart " + strings.Replace(branch, "-", " или ", 1)
	}
	f := Finding{Area: "движок", Level: level.Info, Text: strings.TrimSpace(name), Intrusive: version != ""}
	switch branch {
	case "1.5", "1":
		f.Level, f.Text = level.Bad, name+": ветка без обновлений с 2014 года"
		f.Why = "дыры движка не закрываются, а сам он не работает на PHP 8 - значит, и PHP на сервере без обновлений"
		f.Fix = "переезд на OpenCart 4 или на свой движок с переносом товаров, заказов и адресов страниц"
	case "2":
		f.Level, f.Text = level.Bad, name+": ветка без обновлений с 2016 года"
		f.Why = "дыры движка не закрываются, а сам он не работает на PHP 8 - значит, и PHP на сервере без обновлений"
		f.Fix = "переезд на OpenCart 4 или на свой движок с переносом товаров, заказов и адресов страниц"
	case "2-3":
		f.Why = "вторая ветка не обновляется с 2016 года; точная версия - внизу страницы входа в админку"
	case "":
		return nil
	}
	return []Finding{f}
}

// engine: какой движок; у OpenCart - ветка, а владельцу (Intrusive) - точная версия из админки.
func engine(s *site) []Finding {
	cms := inspect.DetectCMS(s.home.header, cookieNames(s.home.header), s.home.body)
	if cms != "OpenCart" {
		if cms == "" {
			return nil
		}
		return []Finding{{Area: "движок", Level: level.Info, Text: cms}}
	}
	version := ""
	if s.opts.Intrusive {
		version = OpenCartVersion(s.get(s.base+"/admin/", true).body)
	}
	out := OpenCartFindings(OpenCartBranch(s.home.body), version)
	if out == nil {
		return []Finding{{Area: "движок", Level: level.Info, Text: "OpenCart"}}
	}
	return out
}

func cookieNames(h http.Header) []string {
	var out []string
	for _, c := range h.Values("Set-Cookie") {
		name, _, _ := strings.Cut(c, "=")
		out = append(out, strings.TrimSpace(name))
	}
	return out
}
