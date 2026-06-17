package domaudit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/runner"
)

// CacheTTL: сколько ответ логов сертификатов считается свежим. Сервис часто недоступен,
// а состав поддоменов меняется редко.
const CacheTTL = 24 * time.Hour

type crtCache struct {
	Fetched time.Time `json:"fetched"`
	Subs    []string  `json:"subs"`
	Related []string  `json:"related"`
}

func cacheDir() string {
	d := paths.State("crt")
	_ = os.MkdirAll(d, 0o755)
	return d
}

func cachePath(domain string) string {
	safe := strings.Map(func(r rune) rune {
		if r == '.' || r == '-' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' {
			return r
		}
		return '_'
	}, strings.ToLower(domain))
	return filepath.Join(cacheDir(), safe+".json")
}

func readCache(domain string) (crtCache, bool) {
	data, err := os.ReadFile(cachePath(domain))
	if err != nil {
		return crtCache{}, false
	}
	var c crtCache
	if json.Unmarshal(data, &c) != nil || c.Fetched.IsZero() {
		return crtCache{}, false
	}
	return c, true
}

func writeCache(domain string, subs, related []string) {
	data, err := json.Marshal(crtCache{Fetched: time.Now(), Subs: subs, Related: related})
	if err != nil {
		return
	}
	_ = runner.WriteAtomic(cachePath(domain), data, 0o600)
}
