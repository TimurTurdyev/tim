package domaudit

import "slices"

// Откуда имя: подпись источника.
const (
	fromApex     = "апекс"
	fromInside   = "сервер"
	fromHostCert = "сертификат сервера"
	fromSiteCert = "сертификат сайта"
	fromLog      = "журнал сертификатов"
	fromMX       = "MX"
	fromNS       = "NS"
	fromSRV      = "SRV"
	fromSPF      = "SPF"
	fromPTR      = "PTR"
	fromCert     = "сертификат"
	fromPage     = "страница"
	fromScan     = "InternetDB"
	fromBase     = "архив"
	fromWords    = "словарь"
	fromDeep     = "словарь вглубь"
	fromPattern  = "образец"

	sourceTHC          = "THC"
	sourceCenter       = "subdomain.center"
	sourceHackertarget = "hackertarget"
)

var (
	// order: подписи по надёжности - имя подписывается первой из тех, что его знают.
	order = []string{fromApex, fromInside, fromHostCert, fromSiteCert, fromLog, fromMX, fromNS, fromSRV, fromSPF, fromPTR, fromCert,
		fromPage, fromScan, fromBase, sourceTHC, sourceCenter, sourceHackertarget, fromWords, fromDeep, fromPattern}
	// solid: имя точно заведено - сертификат выпускают на то, что есть, а записи и конфиг сервера называют имя прямо.
	solid = []string{fromApex, fromInside, fromHostCert, fromSiteCert, fromLog, fromMX, fromNS, fromSRV, fromSPF, fromPTR, fromCert}
	// outside: подписи сторонних сервисов; остальное нашли сами.
	outside = []string{fromCert, fromScan, sourceTHC, sourceCenter, sourceHackertarget}
	// reported: свои источники, о которых отчёт говорит отдельной строкой, - в этом порядке.
	reported = []string{fromWords, fromDeep, fromBase, fromSPF, fromSRV, fromPTR, fromPage, fromSiteCert, fromHostCert, fromLog, fromPattern,
		fromInside}
)

// rankOf: место подписи по надёжности; незнакомая - в конце.
func rankOf(label string) int {
	if i := slices.Index(order, label); i >= 0 {
		return i
	}
	return len(order)
}

// best: самая надёжная из подписей.
func best(labels []string) string {
	if len(labels) == 0 {
		return ""
	}
	return slices.MinFunc(labels, func(a, b string) int { return rankOf(a) - rankOf(b) })
}

// firm: хотя бы одна подпись говорит, что имя заведено.
func firm(labels []string) bool {
	return slices.ContainsFunc(labels, func(l string) bool { return slices.Contains(solid, l) })
}

// selfFound: имя знает хотя бы один свой источник.
func selfFound(labels []string) bool {
	return slices.ContainsFunc(labels, func(l string) bool { return !slices.Contains(outside, l) })
}
