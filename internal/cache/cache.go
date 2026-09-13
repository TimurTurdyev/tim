// Package cache: ответы медленных и капризных источников в JSON-файлах с датой получения - whois,
// логи сертификатов, сети AS, замер диска.
package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/runner"
)

// Entry: значение и когда его получили.
type Entry[T any] struct {
	Fetched time.Time `json:"fetched"`
	Value   T         `json:"value"`
}

// Fresh: запись моложе ttl.
func (e Entry[T]) Fresh(ttl time.Duration) bool {
	return !e.Fetched.IsZero() && time.Since(e.Fetched) < ttl
}

// Path: файл записи key в каталоге dir. Ключ - домен или номер AS, в имени файла остаются
// только буквы, цифры, точка и дефис.
func Path(dir, key string) string {
	safe := strings.Map(func(r rune) rune {
		if r == '.' || r == '-' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' {
			return r
		}
		return '_'
	}, strings.ToLower(key))
	return filepath.Join(dir, safe+".json")
}

// Load читает запись; ok=false - файла нет, он битый, пустой или в старом формате без value:
// такой надо получить заново, а не считать свежим пустым ответом.
func Load[T any](path string) (Entry[T], bool) {
	var raw struct {
		Fetched time.Time       `json:"fetched"`
		Value   json.RawMessage `json:"value"`
	}
	data, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(data, &raw) != nil || raw.Fetched.IsZero() || len(raw.Value) == 0 {
		return Entry[T]{}, false
	}
	e := Entry[T]{Fetched: raw.Fetched}
	if json.Unmarshal(raw.Value, &e.Value) != nil {
		return Entry[T]{}, false
	}
	return e, true
}

// Store пишет запись целиком или никак. mode 0644 - если файл читают и root, и пользователь.
func Store[T any](path string, v T, mode os.FileMode) error {
	data, err := json.Marshal(Entry[T]{Fetched: time.Now(), Value: v})
	if err != nil {
		return err
	}
	return runner.WriteAtomic(path, data, mode)
}

// Get: свежая запись из файла, иначе fetch и запись. Если fetch не удался, отдаётся протухшая
// запись: старый ответ полезнее пустого, когда источник лежит или ограничивает частоту.
func Get[T any](path string, ttl time.Duration, fetch func() (T, error)) (T, error) {
	old, ok := Load[T](path)
	if ok && old.Fresh(ttl) {
		return old.Value, nil
	}
	v, err := fetch()
	if err != nil {
		if ok {
			return old.Value, nil
		}
		return v, err
	}
	_ = Store(path, v, 0o644)
	return v, nil
}
