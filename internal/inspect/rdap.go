package inspect

import (
	"bytes"
	"compress/gzip"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/TimurTurdyev/tim/internal/cache"
	"github.com/TimurTurdyev/tim/internal/paths"
)

const (
	// registriesURL: список IANA - у какой зоны какой сервер RDAP.
	registriesURL = "https://data.iana.org/rdap/dns.json"
	registriesTTL = 7 * 24 * time.Hour
	maxAnswer     = 1 << 20
)

var (
	//go:embed rdap_dns.json.gz
	registriesBuiltin []byte

	// currentRegistries: список один на процесс, иначе недоступная IANA съедает срок у каждого домена.
	currentRegistries = sync.OnceValue(loadRegistries)

	errNotFound = errors.New("сервер ответил кодом 404")
)

// registries: адрес сервера RDAP по зоне, с "/" на конце.
type registries map[string]string

// base: адрес реестра для имени в ASCII; пусто - у зоны нет RDAP в списке.
func (r registries) base(name string) string {
	return r[lastLabel(name)]
}

// loadRegistries: свежий кэш или IANA; IANA недоступна - прежний кэш, затем встроенная копия.
func loadRegistries() registries {
	path := cache.Path(paths.State("rdap"), "dns")
	if list, err := cache.Get(path, registriesTTL, fetchRegistries); err == nil && len(list) > 0 {
		return list
	}
	list, _ := builtinRegistries()
	return list
}

func fetchRegistries() (registries, error) {
	data, err := getJSON(context.Background(), registriesURL)
	if err != nil {
		return nil, fmt.Errorf("список RDAP не получен: %w", err)
	}
	return parseRegistries(data)
}

func builtinRegistries() (registries, error) {
	packed, err := gzip.NewReader(bytes.NewReader(registriesBuiltin))
	if err != nil {
		return nil, fmt.Errorf("встроенный список RDAP не открыт: %w", err)
	}
	data, err := io.ReadAll(packed)
	if err != nil {
		return nil, fmt.Errorf("встроенный список RDAP не распакован: %w", err)
	}
	return parseRegistries(data)
}

// parseRegistries разбирает dns.json: services - пары [зоны, адреса серверов].
func parseRegistries(data []byte) (registries, error) {
	var list struct {
		Services [][][]string `json:"services"`
	}
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("список RDAP не разобран: %w", err)
	}
	out := registries{}
	for _, service := range list.Services {
		if len(service) < 2 {
			continue
		}
		base := registryBase(service[1])
		if base == "" {
			continue
		}
		for _, zone := range service[0] {
			out[strings.ToLower(zone)] = base
		}
	}
	if len(out) == 0 {
		return nil, errors.New("в списке RDAP нет ни одной зоны")
	}
	return out, nil
}

// registryBase: https раньше http, на конце "/".
func registryBase(urls []string) string {
	for _, scheme := range []string{"https://", "http://"} {
		for _, u := range urls {
			if strings.HasPrefix(u, scheme) {
				return strings.TrimSuffix(u, "/") + "/"
			}
		}
	}
	return ""
}

// getJSON: тело ответа 200 не длиннее maxAnswer; 404 - errNotFound.
func getJSON(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("запрос не собран: %w", err)
	}
	req.Header.Set("Accept", "application/rdap+json, application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("сервер не ответил: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, errNotFound
	default:
		return nil, fmt.Errorf("сервер ответил кодом %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	if err != nil {
		return nil, fmt.Errorf("ответ оборван: %w", err)
	}
	return data, nil
}

func lastLabel(name string) string {
	return name[strings.LastIndexByte(name, '.')+1:]
}
