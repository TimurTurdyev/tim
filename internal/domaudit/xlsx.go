package domaudit

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Таблица Excel без зависимостей: zip с XML. Текст пишется строкой внутри ячейки, поэтому значение вида =cmd
// останется текстом, а не формулой.
const (
	cellLimit = 32767 // больше знаков в ячейку не помещается
	maxColumn = 60

	xlsxTypes = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>%s</Types>`
	xlsxRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`
	xlsxBook = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>%s</sheets></workbook>`
	xlsxBookRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">%s<Relationship Id="rIdS" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`
	xlsxStyles = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts><fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills><borders count="1"><border/></borders><cellStyleXfs count="1"><xf/></cellStyleXfs><cellXfs count="2"><xf fontId="0"/><xf fontId="1" applyFont="1"/></cellXfs></styleSheet>`
)

// sheet: лист таблицы; в ячейках строки и целые числа, ноль - пустая.
type sheet struct {
	name string
	head []string
	rows [][]any
}

// WriteXLSX сохраняет отчёт таблицей: имена, MX и NS, TXT, адреса.
func WriteXLSX(rep *Report, path string) error {
	data, err := encodeXLSX(sheets(rep))
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func sheets(rep *Report) []sheet {
	byIP := map[string]Addr{}
	for _, a := range rep.Addrs {
		byIP[a.IP] = a
	}
	place := func(ips []string) (owner, where string) {
		if len(ips) == 0 {
			return "", ""
		}
		a := byIP[ips[0]]
		if where = a.Country; a.CDN {
			where = "везде"
		}
		return a.AS.Owner(), where
	}
	names := sheet{name: "Имена", head: []string{"Имя", "Ответ", "Адреса", "Сеть", "Где", "Движок", "Сервер", "Заголовок", "Куда ведёт", "CNAME", "Сертификат", "Захват", "Откуда имя"}}
	for _, h := range rep.Hosts {
		owner, where := place(h.IPs)
		takeover := ""
		if h.Takeover != nil {
			takeover = h.Takeover.Service + ": " + h.Takeover.Why
		}
		names.rows = append(names.rows, []any{h.Name, h.Status, strings.Join(h.IPs, ", "), owner, where, h.Engine, h.Server, h.Title, h.Final,
			h.CNAME, h.CertError, takeover, h.Source})
	}
	servers := sheet{name: "MX и NS", head: []string{"Запись", "Сервер", "Приоритет", "Адреса", "Сеть", "Где"}}
	for _, s := range rep.MX {
		owner, where := place(s.IPs)
		servers.rows = append(servers.rows, []any{"MX", s.Name, s.Pref, strings.Join(s.IPs, ", "), owner, where})
	}
	for _, s := range rep.NS {
		owner, where := place(s.IPs)
		servers.rows = append(servers.rows, []any{"NS", s.Name, 0, strings.Join(s.IPs, ", "), owner, where})
	}
	texts := sheet{name: "TXT", head: []string{"Значение"}}
	for _, t := range rep.TXT {
		texts.rows = append(texts.rows, []any{t})
	}
	addrs := sheet{name: "Адреса", head: []string{"Адрес", "PTR", "Подсеть", "AS", "Сеть", "Где", "Свой", "Порты", "Что отвечает", "CVE по версии", "Имена", "MX", "NS", "Соседи"}}
	for _, a := range rep.Addrs {
		own := ""
		if a.Own {
			own = "да"
		}
		_, where := place([]string{a.IP})
		ports, products, cve := "", "", 0
		if s := a.Services; s != nil {
			list := make([]string, len(s.Ports))
			for i, p := range s.Ports {
				list[i] = strconv.Itoa(p)
			}
			ports, products, cve = strings.Join(list, " "), strings.Join(s.Products, ", "), len(s.CVE)
		}
		as := ""
		if a.AS.ASN != "" {
			as = "AS" + a.AS.ASN
		}
		addrs.rows = append(addrs.rows, []any{a.IP, a.PTR, a.AS.Prefix, as, a.AS.Owner(), where, own, ports, products, cve,
			strings.Join(a.Hosts, ", "), strings.Join(a.MX, ", "), strings.Join(a.NS, ", "), strings.Join(a.Shared, ", ")})
	}
	return []sheet{names, servers, texts, addrs}
}

func encodeXLSX(list []sheet) ([]byte, error) {
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	var types, book, rels strings.Builder
	for i, s := range list {
		n := i + 1
		fmt.Fprintf(&types, `<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, n)
		fmt.Fprintf(&book, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, escape(s.name), n, n)
		fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, n, n)
	}
	parts := []struct{ name, body string }{
		{"[Content_Types].xml", fmt.Sprintf(xlsxTypes, types.String())},
		{"_rels/.rels", xlsxRels},
		{"xl/workbook.xml", fmt.Sprintf(xlsxBook, book.String())},
		{"xl/_rels/workbook.xml.rels", fmt.Sprintf(xlsxBookRels, rels.String())},
		{"xl/styles.xml", xlsxStyles},
	}
	for i, s := range list {
		parts = append(parts, struct{ name, body string }{fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1), s.xml()})
	}
	for _, p := range parts {
		w, err := z.Create(p.name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write([]byte(p.body)); err != nil {
			return nil, err
		}
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// xml: лист с закреплённой шапкой, шириной колонок по значениям и фильтром.
func (s sheet) xml() string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n" +
		`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews><cols>`)
	for i, w := range s.widths() {
		fmt.Fprintf(&b, `<col min="%d" max="%d" width="%d" customWidth="1"/>`, i+1, i+1, w)
	}
	b.WriteString(`</cols><sheetData>`)
	head := make([]any, len(s.head))
	for i, h := range s.head {
		head[i] = h
	}
	writeRow(&b, 1, head, 1)
	for i, r := range s.rows {
		writeRow(&b, i+2, r, 0)
	}
	fmt.Fprintf(&b, `</sheetData><autoFilter ref="A1:%s%d"/></worksheet>`, column(len(s.head)-1), len(s.rows)+1)
	return b.String()
}

func (s sheet) widths() []int {
	out := make([]int, len(s.head))
	for i, h := range s.head {
		out[i] = len([]rune(h)) + 4
	}
	for _, r := range s.rows {
		for i, v := range r {
			out[i] = min(maxColumn, max(out[i], len([]rune(fmt.Sprint(v)))+2))
		}
	}
	return out
}

func writeRow(b *strings.Builder, n int, cells []any, style int) {
	fmt.Fprintf(b, `<row r="%d">`, n)
	for i, v := range cells {
		ref := column(i) + strconv.Itoa(n)
		switch x := v.(type) {
		case int:
			if x != 0 {
				fmt.Fprintf(b, `<c r="%s"><v>%d</v></c>`, ref, x)
			}
		case string:
			if x != "" {
				fmt.Fprintf(b, `<c r="%s" s="%d" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, ref, style, escape(x))
			}
		}
	}
	b.WriteString(`</row>`)
}

// column: номер колонки буквами - 0 -> A, 26 -> AA.
func column(i int) string {
	name := ""
	for i++; i > 0; i = (i - 1) / 26 {
		name = string(rune('A'+(i-1)%26)) + name
	}
	return name
}

// escape: текст для XML; символы, которых в XML быть не может, заменяются.
func escape(s string) string {
	if r := []rune(s); len(r) > cellLimit {
		s = string(r[:cellLimit])
	}
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
