// Package ctlog: выпуски сертификатов на домен по журналам Certificate Transparency - через CertSpotter:
// crt.sh глубже по истории, но почти всегда не отвечает за разумное время. Без ключа, кэш 6 часов.
package ctlog

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/TimurTurdyev/tim/internal/cache"
	"github.com/TimurTurdyev/tim/internal/paths"
)

var client = &http.Client{Timeout: 20 * time.Second}

// Issuance: выпущенный сертификат.
type Issuance struct {
	ID        string    `json:"id"`
	Names     []string  `json:"dns_names"`
	Issuer    string    `json:"issuer"` // "Let's Encrypt", "Sectigo"
	NotBefore time.Time `json:"not_before"`
	NotAfter  time.Time `json:"not_after"`
}

// Issuances: действующие сертификаты домена и поддоменов, свежие первыми.
func Issuances(domain string) ([]Issuance, error) {
	return cache.Get(cache.Path(paths.State("ctlog"), domain), 6*time.Hour, func() ([]Issuance, error) { return fetch(domain) })
}

func fetch(domain string) ([]Issuance, error) {
	u := "https://api.certspotter.com/v1/issuances?domain=" + url.QueryEscape(domain) +
		"&include_subdomains=true&expand=dns_names&expand=issuer"
	resp, err := client.Get(u)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("certspotter: %s", resp.Status)
	}
	return Parse(io.LimitReader(resp.Body, 16<<20))
}

// Parse разбирает ответ CertSpotter.
func Parse(r io.Reader) ([]Issuance, error) {
	var raw []struct {
		ID     string   `json:"id"`
		Names  []string `json:"dns_names"`
		Issuer struct {
			Friendly string `json:"friendly_name"`
			Name     string `json:"name"`
		} `json:"issuer"`
		NotBefore time.Time `json:"not_before"`
		NotAfter  time.Time `json:"not_after"`
	}
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, err
	}
	out := make([]Issuance, 0, len(raw))
	for i := len(raw) - 1; i >= 0; i-- {
		x := raw[i]
		issuer := x.Issuer.Friendly
		if issuer == "" {
			issuer = x.Issuer.Name
		}
		out = append(out, Issuance{ID: x.ID, Names: x.Names, Issuer: issuer, NotBefore: x.NotBefore, NotAfter: x.NotAfter})
	}
	return out, nil
}
