package domaudit

import (
	"time"

	"github.com/TimurTurdyev/tim/internal/cache"
	"github.com/TimurTurdyev/tim/internal/paths"
)

// CacheTTL: сколько ответ логов сертификатов считается свежим. Сервис часто недоступен,
// а состав поддоменов меняется редко.
const CacheTTL = 24 * time.Hour

type crtNames struct {
	Subs    []string `json:"subs"`
	Related []string `json:"related"`
}

func cachePath(domain string) string { return cache.Path(paths.State("crt"), domain) }

func readCache(domain string) (cache.Entry[crtNames], bool) {
	return cache.Load[crtNames](cachePath(domain))
}

func writeCache(domain string, subs, related []string) {
	_ = cache.Store(cachePath(domain), crtNames{Subs: subs, Related: related}, 0o600)
}
