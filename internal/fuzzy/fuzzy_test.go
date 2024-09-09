package fuzzy

import "testing"

func TestCyrillicRunes(t *testing.T) {
	items := []string{"Проекты клиентов", "сервер"}
	ms := Filter("клиен", items)
	if len(ms) != 1 || ms[0].Index != 0 || ms[0].Positions[0] != 8 {
		t.Errorf("got %+v", ms)
	}
}
