// Package visits: куда переходили и как давно - чтобы пикер ставил нужное сверху.
package visits

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/TimurTurdyev/tim/internal/paths"
)

// Entry: сколько раз переходили и когда последний.
type Entry struct {
	Count int       `json:"count"`
	Last  time.Time `json:"last"`
}

// Score: частота с поправкой на давность, как у zoxide. Проект, куда ходил сто раз
// полгода назад, уступает тому, где был вчера.
func (e Entry) Score(now time.Time) float64 {
	age := now.Sub(e.Last)
	k := 0.25
	switch {
	case age < time.Hour:
		k = 4
	case age < 24*time.Hour:
		k = 2
	case age < 7*24*time.Hour:
		k = 0.5
	}
	return float64(e.Count) * k
}

func path() string {
	return paths.State("visits.json")
}

// Load читает журнал; без файла - пустой.
func Load() map[string]Entry {
	m := map[string]Entry{}
	if data, err := os.ReadFile(path()); err == nil {
		_ = json.Unmarshal(data, &m)
	}
	return m
}

// Record отмечает переход. Ошибки записи молча проглатываются: журнал - удобство, не данные.
func Record(name string, now time.Time) {
	m := Load()
	e := m[name]
	e.Count++
	e.Last = now
	m[name] = e
	data, err := json.Marshal(m)
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path()), 0o755)
	_ = os.WriteFile(path(), data, 0o600)
}

// Order: индексы names по убыванию счёта; кого не посещали - в исходном порядке после остальных.
func Order(names []string, m map[string]Entry, now time.Time) []int {
	idx := make([]int, len(names))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		return m[names[idx[a]]].Score(now) > m[names[idx[b]]].Score(now)
	})
	return idx
}
