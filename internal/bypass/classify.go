package bypass

import (
	"strings"

	"github.com/TimurTurdyev/tim/internal/netinfo"
)

// Kind: что человек просит пустить напрямую.
type Kind int

const (
	KindUnknown Kind = iota
	KindNet          // адрес, подсеть, as:N
	KindSite         // домен: сайт целиком, с плеером и тем, что он подгружает
	KindApp          // приложение
)

// Classify: сеть видно по форме, приложение - по тому, что оно установлено, остальное с точкой - сайт.
// appExists подставляется снаружи, чтобы решение можно было проверить без диска.
func Classify(arg string, appExists func(string) bool) Kind {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return KindUnknown
	}
	if t, err := netinfo.ParseTarget(arg); err == nil && t.Kind != netinfo.KindDomain {
		return KindNet
	}
	if strings.Contains(arg, "/") || strings.HasSuffix(strings.ToLower(arg), ".app") {
		return KindApp
	}
	if strings.Contains(arg, ".") {
		if appExists(arg) {
			return KindApp
		}
		return KindSite
	}
	if appExists(arg) {
		return KindApp
	}
	return KindUnknown
}
