package health

import (
	"time"

	"github.com/TimurTurdyev/tim/internal/cache"
	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/paths"
)

// CacheTTL: срок регистрации меняется раз в год, а RDAP и whois ограничивают частоту запросов.
const CacheTTL = 24 * time.Hour

// Registration: как inspect.Registration, но с суточным кэшем. При ошибке запроса
// отдаёт протухший кэш, если он есть: старая дата полезнее пустой строки.
func Registration(domain string) (*inspect.Domain, error) {
	return cache.Get(cache.Path(paths.State("whois"), domain), CacheTTL, func() (*inspect.Domain, error) {
		return inspect.Registration(domain)
	})
}
