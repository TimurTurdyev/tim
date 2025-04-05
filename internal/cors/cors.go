// Package cors: почему браузер не пускает запрос с чужого origin - разбор preflight и ответа.
package cors

import (
	"net/http"
	"strings"
)

// Request: что собирается сделать фронтенд.
type Request struct {
	Origin      string
	Method      string
	Headers     []string
	Credentials bool
}

// Problem: что сломает запрос в браузере. Bad - не пройдёт совсем, иначе - риск или мелочь.
type Problem struct {
	Bad  bool
	Text string
}

// safeMethods и safeHeaders не требуют разрешения в preflight.
var safeMethods = map[string]bool{"GET": true, "HEAD": true, "POST": true}
var safeHeaders = map[string]bool{"accept": true, "accept-language": true, "content-language": true, "content-type": true}

func listHas(header, want string) bool {
	for _, v := range strings.Split(header, ",") {
		if strings.EqualFold(strings.TrimSpace(v), want) {
			return true
		}
	}
	return false
}

// NeedsPreflight: браузер сначала пошлёт OPTIONS. content-type считается простым только
// для форм и текста, но для разбора достаточно грубого правила: любые свои заголовки или метод.
func NeedsPreflight(r Request) bool {
	if !safeMethods[strings.ToUpper(r.Method)] {
		return true
	}
	for _, h := range r.Headers {
		if !safeHeaders[strings.ToLower(h)] {
			return true
		}
	}
	return false
}

func checkOrigin(r Request, h http.Header, where string) []Problem {
	acao := h.Get("Access-Control-Allow-Origin")
	switch {
	case acao == "":
		return []Problem{{Bad: true, Text: where + ": нет Access-Control-Allow-Origin - браузер не отдаст ответ странице"}}
	case acao == "*" && r.Credentials:
		return []Problem{{Bad: true, Text: where + ": Access-Control-Allow-Origin: * вместе с куками браузер не принимает, нужен точный origin"}}
	case acao != "*" && acao != r.Origin:
		return []Problem{{Bad: true, Text: where + ": разрешён другой origin - " + acao}}
	}
	var out []Problem
	if r.Credentials && h.Get("Access-Control-Allow-Credentials") != "true" {
		out = append(out, Problem{Bad: true, Text: where + ": нет Access-Control-Allow-Credentials: true - куки не уйдут или ответ не отдадут"})
	}
	if acao != "*" && !listHas(strings.Join(h.Values("Vary"), ","), "Origin") {
		out = append(out, Problem{Text: where + ": origin подставляется, а Vary: Origin нет - кеш или CDN отдадут ответ с чужим origin"})
	}
	return out
}

// Analyze: проблемы по ответам. preflight - nil, если он не нужен или не делался.
func Analyze(r Request, preflightStatus int, preflight, actual http.Header) []Problem {
	var out []Problem
	if preflight != nil {
		if preflightStatus < 200 || preflightStatus > 299 {
			out = append(out, Problem{Bad: true, Text: "preflight OPTIONS отвечает " + http.StatusText(preflightStatus) + " - браузер не пошлёт сам запрос"})
		}
		out = append(out, checkOrigin(r, preflight, "preflight")...)
		methods := preflight.Get("Access-Control-Allow-Methods")
		m := strings.ToUpper(r.Method)
		starCovers := methods == "*" && !r.Credentials
		if !safeMethods[m] && !listHas(methods, m) && !starCovers {
			out = append(out, Problem{Bad: true, Text: "метод " + m + " не разрешён: Access-Control-Allow-Methods - " + dash(methods)})
		}
		allowed := preflight.Get("Access-Control-Allow-Headers")
		for _, hdr := range r.Headers {
			if safeHeaders[strings.ToLower(hdr)] || listHas(allowed, hdr) {
				continue
			}
			// Звёздочка не покрывает Authorization и не работает вместе с куками.
			if allowed == "*" && !r.Credentials && !strings.EqualFold(hdr, "Authorization") {
				continue
			}
			out = append(out, Problem{Bad: true, Text: "заголовок " + hdr + " не разрешён: Access-Control-Allow-Headers - " + dash(allowed)})
		}
	}
	if actual != nil {
		out = append(out, checkOrigin(r, actual, "ответ")...)
	}
	return out
}

func dash(s string) string {
	if s == "" {
		return "не задан"
	}
	return s
}
