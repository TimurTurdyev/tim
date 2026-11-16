package domaudit

import (
	"slices"
	"time"

	"github.com/TimurTurdyev/tim/internal/cache"
	"github.com/TimurTurdyev/tim/internal/paths"
)

const (
	namesTTL = 20 * time.Hour // меньше суток: при ежедневном обходе сутки обновлялись бы через день
	retryTTL = time.Hour      // источник не ответил: столько его не ждём снова
)

// nameList: ответ одного источника. Got - когда он ответил в последний раз; Error - почему не ответил сейчас,
// тогда списки - прошлые.
type nameList struct {
	Subs    []string  `json:"subs,omitempty"`
	Related []string  `json:"related,omitempty"`
	Cut     bool      `json:"cut,omitempty"`
	Got     time.Time `json:"got,omitzero"`
	Error   string    `json:"error,omitempty"`
}

func namesPath(source, domain string) string { return cache.Path(paths.State("names", source), domain) }

func loadNames(source, domain string) (cache.Entry[nameList], bool) {
	return cache.Load[nameList](namesPath(source, domain))
}

func storeNames(source, domain string, n nameList) {
	_ = cache.Store(namesPath(source, domain), n, 0o600)
}

// knownNames: имена домена из кэша всех источников любого возраста - без обращений в сеть.
func knownNames(domain string) []string {
	var out []string
	for _, s := range sources {
		if e, ok := loadNames(s.name, domain); ok {
			out = append(out, e.Value.Subs...)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}
