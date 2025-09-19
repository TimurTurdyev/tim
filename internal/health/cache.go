package health

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/paths"

	"github.com/TimurTurdyev/tim/internal/inspect"
)

// CacheTTL: срок регистрации меняется раз в год, а RDAP и whois ограничивают частоту запросов.
const CacheTTL = 24 * time.Hour

type domainCache struct {
	Fetched time.Time       `json:"fetched"`
	Domain  *inspect.Domain `json:"domain"`
}

func cachePath(domain string) string {
	dir := paths.State("whois")
	if os.MkdirAll(dir, 0o755) != nil {
		return ""
	}
	safe := strings.Map(func(r rune) rune {
		if r == '.' || r == '-' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' {
			return r
		}
		return '_'
	}, strings.ToLower(domain))
	return filepath.Join(dir, safe+".json")
}

// Registration: как inspect.Registration, но с суточным кэшем. При ошибке запроса
// отдаёт протухший кэш, если он есть: старая дата полезнее пустой строки.
func Registration(domain string) (*inspect.Domain, error) {
	path := cachePath(domain)
	var old *inspect.Domain
	if path != "" {
		if data, err := os.ReadFile(path); err == nil {
			var c domainCache
			if json.Unmarshal(data, &c) == nil && c.Domain != nil {
				if time.Since(c.Fetched) < CacheTTL {
					return c.Domain, nil
				}
				old = c.Domain
			}
		}
	}
	d, err := inspect.Registration(domain)
	if err != nil {
		if old != nil {
			return old, nil
		}
		return nil, err
	}
	if path != "" {
		if data, e := json.Marshal(domainCache{Fetched: time.Now(), Domain: d}); e == nil {
			_ = os.WriteFile(path, data, 0o600)
		}
	}
	return d, nil
}
