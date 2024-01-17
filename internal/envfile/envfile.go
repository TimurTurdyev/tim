// Package envfile: чтение значений из .env без подключения парсеров.
package envfile

import (
	"bufio"
	"os"
	"strings"
)

// Parse возвращает последнее значение каждого ключа. Понимает export,
// кавычки обоих видов, хвостовые комментарии и CRLF.
func Parse(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	vals := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, raw, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		vals[key] = clean(raw)
	}
	return vals, sc.Err()
}

func clean(raw string) string {
	raw = strings.TrimLeft(raw, " \t")
	switch {
	case strings.HasPrefix(raw, `"`):
		rest := raw[1:]
		if i := strings.Index(rest, `"`); i >= 0 {
			return rest[:i]
		}
		return rest
	case strings.HasPrefix(raw, `'`):
		rest := raw[1:]
		if i := strings.Index(rest, `'`); i >= 0 {
			return rest[:i]
		}
		return rest
	}
	if i := strings.Index(raw, " #"); i >= 0 {
		raw = raw[:i]
	}
	if i := strings.Index(raw, "\t#"); i >= 0 {
		raw = raw[:i]
	}
	return strings.TrimSpace(raw)
}

// Get возвращает значение ключа и признак, что ключ есть.
func Get(path, key string) (string, bool) {
	vals, err := Parse(path)
	if err != nil {
		return "", false
	}
	v, ok := vals[key]
	return v, ok
}

// Port вытаскивает порт из APP_URL вида http://localhost:8003.
func Port(url string) string {
	i := strings.LastIndex(url, ":")
	if i < 0 {
		return ""
	}
	p := strings.TrimRight(url[i+1:], "/")
	for _, c := range p {
		if c < '0' || c > '9' {
			return ""
		}
	}
	return p
}
