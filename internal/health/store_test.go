package health

import (
	"testing"

	"github.com/TimurTurdyev/tim/internal/level"
)

// Регрессия: уровень писался в кэш словом, а читался числом - кэш проверок не читался вовсе.
func TestSavedRowRoundTrip(t *testing.T) {
	t.Setenv("TIM_STATE_DIR", t.TempDir())
	SaveRows([]Row{{Name: "shop", Checks: []Check{{Name: "почта", Level: level.Bad, Text: "плохо"}}}})
	got, ok := SavedRow("shop")
	if !ok || got.Get("почта").Level != level.Bad {
		t.Fatalf("из кэша: %+v %v", got, ok)
	}
	// Проверка без сети не стирает сетевые ответы.
	SaveRows([]Row{{Name: "shop", Checks: []Check{{Name: "git", Level: level.OK}, {Name: "почта", Level: level.Skip}}}})
	got, _ = SavedRow("shop")
	if got.Get("почта").Level != level.Bad || got.Get("git").Level != level.OK {
		t.Errorf("после проверки без сети: %+v", got.Checks)
	}
}
