// Package harvest: имена домена из того, что отдаёт сама цель - страницы, заголовки ответа, записи DNS.
package harvest

import (
	"html"
	"maps"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"
)

const (
	maxLabel = 63
	maxName  = 253
)

// PageNames: домен и его поддомены из тела страницы и заголовков ответа - в нижнем регистре, по алфавиту.
func PageNames(domain, body string, header http.Header) []string {
	domain = hostName(domain)
	if domain == "" {
		return nil
	}
	seen := map[string]bool{}
	collect(seen, body, domain)
	for key, values := range header {
		cookie := http.CanonicalHeaderKey(key) == "Set-Cookie"
		for _, value := range values {
			if cookie {
				value = cookieDomain(value)
			}
			collect(seen, value, domain)
		}
	}
	return slices.Sorted(maps.Keys(seen))
}

// cookieDomain: значение Domain= у куки; в самом значении куки имена случайны.
func cookieDomain(cookie string) string {
	_, attrs, _ := strings.Cut(cookie, ";")
	for attr := range strings.SplitSeq(attrs, ";") {
		key, value, _ := strings.Cut(attr, "=")
		if strings.EqualFold(strings.TrimSpace(key), "domain") {
			return value
		}
	}
	return ""
}

// collect кладёт в seen имена из text, которые кончаются на domain.
func collect(seen map[string]bool, text, domain string) {
	text = lowerASCII(unescape(text))
	for from := 0; from < len(text); {
		i := strings.Index(text[from:], domain)
		if i < 0 {
			return
		}
		at := from + i
		end := at + len(domain)
		from = at + 1
		start, ok := nameStart(text, at, end)
		if !ok || !nameEnds(text, end) {
			continue
		}
		if name := text[start:end]; !seen[name] {
			seen[strings.Clone(name)] = true // срез держал бы в памяти всю страницу
		}
		from = end
	}
}

// nameStart: где начинается имя с доменом в text[at:end]; ok=false - домен здесь хвост чужой метки или имя длиннее 253.
func nameStart(text string, at, end int) (start int, ok bool) {
	if at == 0 {
		return 0, true
	}
	if text[at-1] != '.' {
		return at, !labelByte(text[at-1])
	}
	start = at
	for start > 0 && text[start-1] == '.' {
		dot := start - 1
		low := dot
		// Предел шага: без него страница из одних меток и точек разбиралась бы за квадрат своей длины.
		for low > 0 && dot-low <= maxName && labelByte(text[low-1]) {
			low--
		}
		for low < dot && text[low] == '-' {
			low++
		}
		if !validLabel(text[low:dot]) {
			break
		}
		if start = low; end-start > maxName {
			return start, false
		}
	}
	return start, true
}

// nameEnds: за доменом имя не продолжается - example.com.evil.test и example.community чужие.
func nameEnds(text string, end int) bool {
	if end == len(text) {
		return true
	}
	if labelByte(text[end]) {
		return false
	}
	return text[end] != '.' || end+1 == len(text) || !labelByte(text[end+1])
}

// unescape снимает запись, из-за которой "%2Fwww.example.com" читается как имя 2fwww; два прохода - ради %252F.
func unescape(text string) string {
	if strings.Contains(text, "&") {
		text = html.UnescapeString(text)
	}
	for range 2 {
		text = unescapeOnce(text)
	}
	return text
}

func unescapeOnce(text string) string {
	const marks = `%\`
	i := strings.IndexAny(text, marks)
	if i < 0 {
		return text
	}
	var out strings.Builder
	out.Grow(len(text))
	for ; i >= 0; i = strings.IndexAny(text, marks) {
		char, size := decode(text[i:])
		out.WriteString(text[:i])
		out.WriteByte(char)
		text = text[i+size:]
	}
	out.WriteString(text)
	return out.String()
}

// decode: знак, записанный в начале text как %2F, \x2f, \u002f или \/, и длина этой записи.
func decode(text string) (byte, int) {
	if text[0] == '%' {
		if char, ok := hexChar(text[1:], 2); ok {
			return char, 3
		}
		return '%', 1
	}
	if len(text) < 2 {
		return text[0], 1
	}
	switch text[1] {
	case '/':
		return '/', 2
	case 'n', 'r', 't':
		return ' ', 2
	case 'x':
		if char, ok := hexChar(text[2:], 2); ok {
			return char, 4
		}
	case 'u':
		if char, ok := hexChar(text[2:], 4); ok {
			return char, 6
		}
	}
	return text[0], 1
}

// hexChar: знак из первых width шестнадцатеричных цифр; вне печатного ASCII - пробел, в имени такого знака нет.
func hexChar(text string, width int) (byte, bool) {
	if len(text) < width {
		return 0, false
	}
	code, err := strconv.ParseUint(text[:width], 16, 16)
	if err != nil {
		return 0, false
	}
	if code <= ' ' || code > '~' {
		return ' ', true
	}
	return byte(code), true
}

// lowerASCII не трогает буквы вне ASCII: strings.ToLower делает из знака кельвина "k", и граница имени сдвигается.
func lowerASCII(text string) string {
	first := strings.IndexAny(text, "ABCDEFGHIJKLMNOPQRSTUVWXYZ")
	if first < 0 {
		return text
	}
	out := []byte(text)
	for i := first; i < len(out); i++ {
		if c := out[i]; c >= 'A' && c <= 'Z' {
			out[i] = c + 'a' - 'A'
		}
	}
	return string(out)
}

// hostName: имя в нижнем регистре без точки на конце; пусто, если это не имя из двух и более меток.
func hostName(name string) string {
	name = lowerASCII(strings.TrimSuffix(name, "."))
	if len(name) > maxName || !strings.Contains(name, ".") {
		return ""
	}
	for label := range strings.SplitSeq(name, ".") {
		if !validLabel(label) {
			return ""
		}
	}
	if _, err := netip.ParseAddr(name); err == nil {
		return ""
	}
	return name
}

func validLabel(label string) bool {
	if label == "" || len(label) > maxLabel || label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}
	for i := 0; i < len(label); i++ {
		if !labelByte(label[i]) {
			return false
		}
	}
	return true
}

// labelByte: знак метки; подчёркивание бывает в служебных именах вроде _dmarc.
func labelByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_'
}
