package domaudit

import (
	"slices"

	"github.com/TimurTurdyev/tim/internal/cache"
	"github.com/TimurTurdyev/tim/internal/paths"
)

// memory: живые имена, которые свои источники находили раньше, - по подписям. Быстрый и ночной сбор берут
// отсюда то, что видит только полный: страницы, сертификаты, размножение.
type memory struct {
	Names map[string][]string `json:"names"`
}

func memoryPath(domain string) string { return cache.Path(paths.State("names", "own"), domain) }

// recall: прошлые находки своих источников снова идут на проверку - с теми же подписями.
func (c *collector) recall() {
	e, ok := cache.Load[memory](memoryPath(c.domain))
	if !ok {
		return
	}
	for label, names := range e.Value.Names {
		if !slices.Contains(reported, label) {
			continue
		}
		for _, n := range names {
			c.learn(n, label)
			c.recalled[n] = append(c.recalled[n], label)
		}
	}
}

// remember: что из названного своими источниками живо сейчас. Пустой сбор память не трогает: это сбой DNS.
func (c *collector) remember() {
	m := memory{Names: map[string][]string{}}
	for name, labels := range c.from {
		if h := c.hosts[name]; h == nil || len(h.IPs) == 0 {
			continue
		}
		for _, l := range labels {
			if slices.Contains(reported, l) {
				m.Names[l] = append(m.Names[l], name)
			}
		}
	}
	if len(m.Names) == 0 {
		return
	}
	for _, names := range m.Names {
		slices.Sort(names)
	}
	_ = cache.Store(memoryPath(c.domain), m, 0o600)
}
