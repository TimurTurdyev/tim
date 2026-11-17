package domaudit

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"testing"
)

// Значения пишут чужие люди: лист остаётся правильным XML, формула - текстом, управляющие символы не проходят.
func TestEncodeXLSX(t *testing.T) {
	values := []string{"a & b", "<script>", "=cmd|' /C calc'!A0", "esc\x1b[31m", "перенос\nстроки", "обычный"}
	s := sheet{name: "Имена & адреса", head: []string{"Имя", "Ответ"}}
	for i, v := range values {
		s.rows = append(s.rows, []any{v, i * 100})
	}
	data, err := encodeXLSX([]sheet{s, {name: "Пусто", head: []string{"Значение"}}})
	if err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Rows []struct {
			Cells []struct {
				Ref   string `xml:"r,attr"`
				Type  string `xml:"t,attr"`
				Text  string `xml:"is>t"`
				Value string `xml:"v"`
			} `xml:"c"`
		} `xml:"sheetData>row"`
	}
	for _, f := range z.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(r)
		var target any = &struct{}{}
		if f.Name == "xl/worksheets/sheet1.xml" {
			target = &got
		}
		if err := xml.Unmarshal(body, target); err != nil {
			t.Errorf("%s: %v", f.Name, err)
		}
	}
	if len(got.Rows) != len(values)+1 {
		t.Fatalf("строк %d", len(got.Rows))
	}
	want := append([]string{}, values...)
	want[3] = "esc\uFFFD[31m"
	for i, v := range want {
		c := got.Rows[i+1].Cells[0]
		if c.Type != "inlineStr" || c.Text != v {
			t.Errorf("строка %d: %q (%s), ждали %q", i, c.Text, c.Type, v)
		}
	}
	if len(got.Rows[1].Cells) != 1 || got.Rows[2].Cells[1].Value != "100" || got.Rows[2].Cells[1].Ref != "B3" {
		t.Errorf("числа: %+v", got.Rows[1:3])
	}
	for i, name := range map[int]string{0: "A", 25: "Z", 26: "AA", 701: "ZZ", 702: "AAA"} {
		if column(i) != name {
			t.Errorf("колонка %d: %s", i, column(i))
		}
	}
}
