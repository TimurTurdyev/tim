package domaudit

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

var (
	charsetRe = regexp.MustCompile(`(?i)charset\s*=\s*["']?\s*([a-z0-9_-]+)`)
	// Кодировки старых русских сайтов; остальные читаются как есть.
	charsets = map[string]*charmap.Charmap{
		"windows-1251": charmap.Windows1251, "cp1251": charmap.Windows1251, "win-1251": charmap.Windows1251,
		"koi8-r": charmap.KOI8R, "koi8-u": charmap.KOI8U, "iso-8859-5": charmap.ISO8859_5, "cp866": charmap.CodePage866,
	}
)

// decode: страница в UTF-8 - кодировка из заголовка Content-Type, иначе из meta.
func decode(body []byte, contentType string) string {
	if utf8.Valid(body) {
		return string(body)
	}
	for _, where := range []string{contentType, string(body[:min(len(body), 2048)])} {
		m := charsetRe.FindStringSubmatch(where)
		if m == nil {
			continue
		}
		if cs := charsets[strings.ToLower(m[1])]; cs != nil {
			if out, err := cs.NewDecoder().Bytes(body); err == nil {
				return string(out)
			}
		}
		break
	}
	return string(body)
}
