package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Регрессия: файл прежнего формата ({"fetched","prefixes"}) читался свежей записью с пустым
// значением, и сети AS сутки раскрывались в ничто. Теперь это промах и запрос заново.
func TestOldFormatIsMiss(t *testing.T) {
	path := filepath.Join(t.TempDir(), "13238.json")
	old := `{"fetched":"` + time.Now().Format(time.RFC3339) + `","prefixes":["192.0.2.0/24"]}`
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := Load[[]string](path); ok {
		t.Fatal("старый формат прочитан как запись")
	}
	calls := 0
	got, err := Get(path, time.Hour, func() ([]string, error) {
		calls++
		return []string{"198.51.100.0/24"}, nil
	})
	if err != nil || calls != 1 || len(got) != 1 || got[0] != "198.51.100.0/24" {
		t.Fatalf("Get по старому файлу: %v %v, запросов %d", got, err, calls)
	}
	e, ok := Load[[]string](path)
	if !ok || len(e.Value) != 1 || !e.Fresh(time.Hour) {
		t.Errorf("после Get файл не в новом формате: %+v %v", e, ok)
	}
}
