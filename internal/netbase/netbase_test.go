package netbase

import (
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stage кладёт образцы под именами для Build, дампы RIS сжимает; change заменяет файл, nil в нём - файла нет.
func stage(t *testing.T, change map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()
	samples := map[string]string{risFile4: "ris4.txt", risFile6: "ris6.txt", namesFile: "asn.txt",
		countryFile4: "country4.csv", countryFile6: "country6.csv"}
	for name, sample := range samples {
		data, changed := change[name]
		if changed && data == nil {
			continue
		}
		if !changed {
			var err error
			if data, err = os.ReadFile(filepath.Join("testdata", sample)); err != nil {
				t.Fatal(err)
			}
			if strings.HasSuffix(name, ".gz") {
				data = packed(t, data)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func packed(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func dash(s string) string {
	if s == "-" {
		return ""
	}
	return s
}

// Три формата по образцам, самая узкая сеть, MOAS и отсев - одной таблицей ожидаемых ответов.
func TestLookup(t *testing.T) {
	dir := t.TempDir()
	built := time.Date(2026, 11, 25, 3, 0, 0, 0, time.UTC)
	stats, err := build(stage(t, nil), dir, built)
	if err != nil {
		t.Fatal(err)
	}
	want := Stats{Nets4: 13, Nets6: 4, Names: 11, Countries4: 5, Countries6: 4}
	for _, spec := range specs {
		file, err := os.Stat(filepath.Join(dir, spec.file))
		if err != nil {
			t.Fatal(err)
		}
		want.Bytes += file.Size()
	}
	if stats != want {
		t.Errorf("счёт сборки: %+v, ждали %+v", stats, want)
	}
	tables, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !tables.Built().Equal(built) {
		t.Errorf("время сборки: %v", tables.Built())
	}
	data, err := os.ReadFile(filepath.Join("testdata", "lookup.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.Lines(string(data)) {
		f := strings.Split(strings.TrimSuffix(line, "\n"), "\t")
		if strings.HasPrefix(line, "#") || len(f) != 7 {
			continue
		}
		known := Info{Prefix: dash(f[1]), ASN: dash(f[2]), MOAS: f[3] == "+", Country: dash(f[4]),
			Registered: dash(f[5]), Name: dash(f[6])}
		got, ok := tables.Lookup(f[0])
		if got != known || ok != (known != Info{}) {
			t.Errorf("%s: %+v %v, ждали %+v", f[0], got, ok, known)
		}
	}
	if _, ok := tables.Lookup("не адрес"); ok {
		t.Error("строка без адреса нашлась")
	}
}

func TestParseRoute(t *testing.T) {
	cases := []struct {
		line   string
		keyLen int
		want   lineKind
	}{
		{"4199990001\t192.0.2.0/24\t400", 4, lineKept},
		{"4199990001\t10.0.0.0/8\t10", 4, lineKept},
		{"{4199990001}\t198.19.5.0/24\t310", 4, lineKept},
		{"4199990001\t2001:db8::/48\t10", 16, lineKept},
		{"4199990001\t3fff::/16\t10", 16, lineKept},
		{"4199990001\t0.0.0.0/0\t400", 4, lineDropped},
		{"4199990002\t240.0.0.0/7\t300", 4, lineDropped},
		{"4199990002\t192.0.2.128/25\t400", 4, lineDropped},
		{"4199990003\t192.0.2.200/32\t400", 4, lineDropped},
		{"4199990004\t198.18.6.0/24\t9", 4, lineDropped},
		{"0\t198.19.3.0/24\t200", 4, lineDropped},
		{"23456\t198.19.4.0/24\t200", 4, lineDropped},
		{"64512\t198.19.3.0/24\t200", 4, lineDropped},
		{"4200000001\t198.19.3.0/24\t200", 4, lineDropped},
		{"{4199990001,4199990002}\t198.19.6.0/24\t300", 4, lineDropped},
		{"4199990001\t::/0\t400", 16, lineDropped},
		{"4199990001\t2000::/3\t400", 16, lineDropped},
		{"4199990003\t2001:db8:100:1::/64\t400", 16, lineDropped},
		{"4199990001\t198.19.7.1/24\t200", 4, lineBroken},
		{"4199990001 198.19.8.0/24 200", 4, lineBroken},
		{"4199990001\t198.19.8.0/24", 4, lineBroken},
		{"4199990001\t198.19.8.0/24\tмного", 4, lineBroken},
		{"4199990001\t198.19.8.0/24\t200\t1", 4, lineBroken},
		{"AS4199990001\t198.19.8.0/24\t200", 4, lineBroken},
		{"4199990001\t2001:db8::/32\t200", 4, lineBroken},
		{"4199990001\t192.0.2.0/24\t200", 16, lineBroken},
		{"<html>", 4, lineBroken},
	}
	for _, c := range cases {
		if _, got := parseRoute([]byte(c.line), c.keyLen); got != c.want {
			t.Errorf("%q: %d, ждали %d", c.line, got, c.want)
		}
	}
}

func TestASName(t *testing.T) {
	cases := []struct{ raw, name, registered string }{
		{"EXAMPLE-NET - Example Networks, Inc., US", "EXAMPLE-NET - Example Networks, Inc., US", "US"},
		{"EXAMPLE-HOSTING Example Hosting GmbH, DE", "EXAMPLE-HOSTING - Example Hosting GmbH, DE", "DE"},
		{`EXAMPLE-RU "Example Telecom", Ltd, RU`, `EXAMPLE-RU - "Example Telecom", Ltd, RU`, "RU"},
		{`EXAMPLE-RU LLC "Example - Telecom", RU`, `EXAMPLE-RU - LLC "Example - Telecom", RU`, "RU"},
		{"EXAMPLE-LAB - Example Corporation - Research Laboratory, US", "EXAMPLE-LAB - Example Corporation - Research Laboratory, US", "US"},
		{"Exemple Côte - Exemple Côte d'Ivoire, CI", "Exemple Côte - Exemple Côte d'Ivoire, CI", "CI"},
		{"EXAMPLE-DE Example Luft- und Raumfahrt e.V., DE", "EXAMPLE-DE - Example Luft- und Raumfahrt e.V., DE", "DE"},
		{"EXAMPLE-AP - Example Pty Ltd, Level 3, AU", "EXAMPLE-AP - Example Pty Ltd, Level 3, AU", "AU"},
		{"EXAMPLE-IT  Example  S.p.A., IT", "EXAMPLE-IT - Example S.p.A., IT", "IT"},
		{"Examplemyndigheten, SE", "Examplemyndigheten, SE", "SE"},
		{"AS64500-138 - , KR", "AS64500-138, KR", "KR"},
		{"- , NZ", "", "NZ"},
		{"EXAMPLE-OLD Example Networks", "EXAMPLE-OLD - Example Networks", ""},
		{"", "", ""},
	}
	for _, c := range cases {
		if name, registered := asName(c.raw, nil); name != c.name || registered != c.registered {
			t.Errorf("%q: %q %q, ждали %q %q", c.raw, name, registered, c.name, c.registered)
		}
	}
}

func TestParseCountry(t *testing.T) {
	cases := []struct {
		name, text   string
		keyLen       int
		rows, broken int
	}{
		{"соседние диапазоны", "192.0.2.0,192.0.2.127,NL\n192.0.2.128,192.0.2.255,US\n", 4, 2, 0},
		{"не по порядку", "198.51.100.0,198.51.100.255,DE\n192.0.2.0,192.0.2.255,NL\n", 4, 1, 1},
		{"пересечение", "192.0.2.0,192.0.2.255,NL\n192.0.2.128,192.0.2.255,US\n", 4, 1, 1},
		{"конец раньше начала", "192.0.2.255,192.0.2.0,NL\n", 4, 0, 1},
		{"код из трёх букв", "192.0.2.0,192.0.2.255,NLD\n", 4, 0, 1},
		{"код строчными", "192.0.2.0,192.0.2.255,nl\n", 4, 0, 1},
		{"нет страны", "192.0.2.0,192.0.2.255\n", 4, 0, 1},
		{"лишнее поле", "192.0.2.0,192.0.2.255,NL,1\n", 4, 0, 1},
		{"IPv6 в наборе IPv4", "2001:db8::,2001:db8::ffff,NL\n", 4, 0, 1},
		{"IPv4 в наборе IPv6", "192.0.2.0,192.0.2.255,NL\n", 16, 0, 1},
		{"IPv6", "2001:db8::,2001:db8::ffff,NL\r\n\r\n2001:db8::1:0,2001:db8::1:ffff,DE\r\n", 16, 2, 0},
	}
	for _, c := range cases {
		sec, err := parseCountry(strings.NewReader(c.text), c.keyLen)
		if err != nil || sec.rows != c.rows || sec.broken != c.broken {
			t.Errorf("%s: годных %d, битых %d, ошибка %v; ждали %d и %d", c.name, sec.rows, sec.broken, err, c.rows, c.broken)
		}
	}
}

// Негодный вход - ошибка, и прежние таблицы отвечают как раньше; годный их заменяет.
func TestBuildKeepsTables(t *testing.T) {
	dir := t.TempDir()
	built := time.Date(2026, 11, 25, 3, 0, 0, 0, time.UTC)
	if _, err := build(stage(t, nil), dir, built); err != nil {
		t.Fatal(err)
	}
	before, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := before.Lookup("192.0.2.1")
	ris4, err := os.ReadFile(filepath.Join("testdata", "ris4.txt"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		change map[string][]byte
	}{
		{"сетей меньше 80% прежнего", map[string][]byte{risFile4: []byte("4199990001\t192.0.2.0/24\t400\n4199990002\t198.51.100.0/24\t420\n")}},
		{"битых строк больше сотой доли", map[string][]byte{risFile4: append([]byte("4199990001 198.19.8.0/24 200\n"), ris4...)}},
		{"оборванный gzip", map[string][]byte{risFile6: packed(t, ris4)[:40]}},
		{"страница вместо имён", map[string][]byte{namesFile: []byte("<html>\n<body>502 Bad Gateway</body>\n</html>\n")}},
		{"страны не по порядку", map[string][]byte{countryFile4: []byte("198.51.100.0,198.51.100.255,DE\n192.0.2.0,192.0.2.255,NL\n")}},
		{"пустой файл", map[string][]byte{countryFile6: {}}},
		{"файла нет", map[string][]byte{countryFile6: nil}},
	}
	for _, c := range cases {
		if _, err := build(stage(t, c.change), dir, built.Add(24*time.Hour)); err == nil {
			t.Errorf("%s: сборка прошла", c.name)
		}
		after, err := Open(dir)
		if err != nil {
			t.Fatalf("%s: прежние таблицы не открылись: %v", c.name, err)
		}
		if got, _ := after.Lookup("192.0.2.1"); got != want || !after.Built().Equal(built) {
			t.Errorf("%s: прежние таблицы изменились: %+v, собраны %v", c.name, got, after.Built())
		}
		if files, _ := os.ReadDir(dir); len(files) != tableCount {
			t.Errorf("%s: в каталоге %d файлов, остались временные", c.name, len(files))
		}
	}
	if _, err := build(stage(t, nil), dir, built.Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if after, err := Open(dir); err != nil || !after.Built().Equal(built.Add(48*time.Hour)) {
		t.Errorf("годный вход таблицы не заменил: %v", err)
	}
}

func TestOpen(t *testing.T) {
	if _, err := Open(filepath.Join(t.TempDir(), "нет")); !errors.Is(err, ErrNoTables) {
		t.Errorf("каталога нет: %v", err)
	}
	dir := t.TempDir()
	if _, err := build(stage(t, nil), dir, time.Unix(0, 0)); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, specs[cc6].file)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data[:len(data)-1], 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err == nil || errors.Is(err, ErrNoTables) {
		t.Errorf("обрезанная таблица: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); !errors.Is(err, ErrNoTables) {
		t.Errorf("неполный набор: %v", err)
	}
}
