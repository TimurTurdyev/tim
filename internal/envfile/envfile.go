// Package envfile: чтение значений из .env без подключения парсеров.
package envfile

import (
	"bufio"
	"os"
	"sort"
	"strings"
)

// Parse возвращает последнее значение каждого ключа. Понимает export,
// кавычки обоих видов, хвостовые комментарии и CRLF.
func Parse(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
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

// Delta: чем файл отличается от образца.
type Delta struct {
	Missing []string // есть в образце, нет в файле
	Extra   []string // есть в файле, нет в образце
	Sample  []string // секрет остался таким же, как в образце
	Empty   []string // ключ есть, значение пустое, а в образце заполнено
}

// Clean: расхождений нет.
func (d Delta) Clean() bool {
	return len(d.Missing)+len(d.Extra)+len(d.Sample)+len(d.Empty) == 0
}

// Diff сравнивает .env с образцом. Значения сравниваются только у секретов:
// у остальных ключей совпадение с образцом - обычное дело.
func Diff(env, example map[string]string) Delta {
	var d Delta
	for k, want := range example {
		got, ok := env[k]
		switch {
		case !ok:
			d.Missing = append(d.Missing, k)
		case got == "" && want != "":
			d.Empty = append(d.Empty, k)
		case Secret(k) && got == want && !placeholder(want):
			d.Sample = append(d.Sample, k)
		}
	}
	for k := range env {
		if _, ok := example[k]; !ok {
			d.Extra = append(d.Extra, k)
		}
	}
	sort.Strings(d.Missing)
	sort.Strings(d.Extra)
	sort.Strings(d.Sample)
	sort.Strings(d.Empty)
	return d
}

// placeholder: значение, которое ничего не скрывает, - у Laravel так пишут "пароля нет".
func placeholder(v string) bool {
	switch strings.ToLower(v) {
	case "", "null", "none", "false", "true", "nil", "-":
		return true
	}
	return false
}

// Keys: отсортированные ключи набора.
func Keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// OnlyIn: ключи, которые есть в a и нет в b.
func OnlyIn(a, b map[string]string) []string {
	var out []string
	for k := range a {
		if _, ok := b[k]; !ok {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
