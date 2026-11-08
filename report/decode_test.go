package report

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDecode(t *testing.T) {
	in := `{"title": "бэкапы", "verdict": {"level": "warn", "text": "один старый"},
	"data": {"count": 2},
	"blocks": [
		{"facts": [{"key": "сервер", "value": "db1"}, {"key": "место", "value": "92%", "level": "bad"}]},
		{"table": {"columns": [{"title": "БАЗА", "style": "key"}, {"title": "РАЗМЕР", "kind": "bytes"},
			{"title": "ВОЗРАСТ", "kind": "duration"}, {"title": "СОСТОЯНИЕ"}],
			"rows": [["shop", 1048576, 90, {"text": "старый", "level": "warn"}]]}},
		{"findings": [{"area": "диск", "level": "bad", "text": "кончается", "fix": "почистить логи"}]},
		{"heading": "дальше"}, {"note": "проверено в 03:00"},
		{"next": [{"text": "освободить", "command": "tim disk"}]}
	]}`
	r, err := Decode(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if r.Title != "бэкапы" || r.Verdict.Level != Warn || string(r.Data.(json.RawMessage)) != `{"count": 2}` || len(r.Blocks) != 6 {
		t.Fatalf("отчёт: %+v", r)
	}
	if f := r.Blocks[0].(Facts); f[1].Level != Bad {
		t.Errorf("уровень факта: %+v", f)
	}
	row := r.Blocks[1].(Table).Rows[0]
	if row[1] != int64(1048576) || row[2] != 90*time.Second || row[3] != (Mark{Text: "старый", Level: Warn}) {
		t.Errorf("ячейки: %#v", row)
	}
	if f := r.Blocks[2].(Findings); f[0].Fix != "почистить логи" {
		t.Errorf("находки: %+v", f)
	}
	for _, bad := range []string{`{"blocks": [{"chart": 1}]}`, `{"blocks": [{"note": "a", "heading": "b"}]}`,
		`{"blocks": [{"table": {"columns": [{"title": "X", "kind": "weight"}]}}]}`, `не json`} {
		if _, err := Decode(strings.NewReader(bad)); err == nil {
			t.Errorf("принят плохой отчёт: %s", bad)
		}
	}
}
