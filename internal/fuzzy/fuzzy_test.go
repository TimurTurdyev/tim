package fuzzy

import "testing"

func names(items []string, ms []Match) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = items[m.Index]
	}
	return out
}

func TestEmptyQueryKeepsOrder(t *testing.T) {
	items := []string{"b", "a", "c"}
	got := names(items, Filter("", items))
	if len(got) != 3 || got[0] != "b" || got[2] != "c" {
		t.Errorf("order: %v", got)
	}
}

func TestDropsNonMatching(t *testing.T) {
	items := []string{"marketplace", "cargo", "website"}
	got := names(items, Filter("mrc", items))
	if len(got) != 1 || got[0] != "marketplace" {
		t.Errorf("got %v", got)
	}
}

func TestCaseInsensitive(t *testing.T) {
	items := []string{"AcmeSite"}
	if len(Filter("acms", items)) != 1 {
		t.Error("case should not matter")
	}
}

func TestPrefixBeatsMiddle(t *testing.T) {
	items := []string{"telecom", "lk-telecom-old", "tel"}
	got := names(items, Filter("tel", items))
	if got[0] != "tel" || got[1] != "telecom" {
		t.Errorf("got %v", got)
	}
}

func TestPathSegmentBeatsScattered(t *testing.T) {
	items := []string{"e-fm-mapper-x", "work/acme/efm"}
	got := names(items, Filter("efm", items))
	if got[0] != "work/acme/efm" {
		t.Errorf("got %v", got)
	}
}

func TestRunBeatsScattered(t *testing.T) {
	items := []string{"cxaxrxgxo", "cargo"}
	got := names(items, Filter("cargo", items))
	if got[0] != "cargo" {
		t.Errorf("got %v", got)
	}
}

func TestBestStartIsChosen(t *testing.T) {
	ms := Filter("site", []string{"simple-site"})
	if len(ms) != 1 || ms[0].Positions[0] != 7 {
		t.Errorf("positions: %+v", ms)
	}
}

func TestCyrillicRunes(t *testing.T) {
	items := []string{"Проекты клиентов", "сервер"}
	ms := Filter("клиен", items)
	if len(ms) != 1 || ms[0].Index != 0 || ms[0].Positions[0] != 8 {
		t.Errorf("got %+v", ms)
	}
}
