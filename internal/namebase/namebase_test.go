package namebase

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const (
	sampleRelease = "cc-main-2099-jan-feb-mar"
	nextRelease   = "cc-main-2099-feb-mar-apr"
)

var (
	sampleParts = []string{
		"part-00000-00000000-0000-4000-8000-000000000000-c000.txt.gz",
		"part-00001-00000000-0000-4000-8000-000000000000-c000.txt.gz",
	}
	errCut = errors.New("связь оборвалась")
)

// pack: часть исходника в виде списка хостов - "номер, табуляция, имя задом наперёд", gzip.
func pack(t *testing.T, revs ...string) []byte {
	t.Helper()
	var text strings.Builder
	for i, rev := range revs {
		fmt.Fprintf(&text, "%d\t%s\n", i, rev)
	}
	return gz(t, text.String())
}

func gz(t *testing.T, text string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte(text)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// serve отдаёт части по номеру; до части stop дело не доходит - связь оборвалась.
func serve(stop int, asked *[]int, parts ...[]byte) func(int) (io.ReadCloser, string, error) {
	return func(i int) (io.ReadCloser, string, error) {
		if asked != nil {
			*asked = append(*asked, i)
		}
		if i == stop {
			return nil, "", errCut
		}
		return io.NopCloser(bytes.NewReader(parts[i])), "", nil
	}
}

func install(t *testing.T, dir string, block int, release string, parts ...[]byte) {
	t.Helper()
	if err := update(dir, block, Release{ID: release}, len(parts), serve(-1, nil, parts...), nil); err != nil {
		t.Fatal(err)
	}
}

func lookup(t *testing.T, dir, domain string, limit int) ([]string, int) {
	t.Helper()
	base, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	names, total, err := base.Lookup(domain, limit)
	if err != nil {
		t.Fatal(err)
	}
	return names, total
}

// under: тот же ответ, что у Lookup, прямым перебором списка.
func under(domain string, revs []string) []string {
	rev := flip(domain)
	var names []string
	for _, name := range revs {
		if name == rev || strings.HasPrefix(name, rev+".") {
			names = append(names, flip(name))
		}
	}
	return names
}

// same: поиск по каждому домену отдаёт то же, что прямой перебор списка revs, с пределом и без.
func same(t *testing.T, dir, step string, revs []string, domains ...string) {
	t.Helper()
	for _, domain := range domains {
		want := under(domain, revs)
		if names, total := lookup(t, dir, domain, 100); total != len(want) || !slices.Equal(names, want) {
			t.Errorf("%s, %s: всего %d, имена %v, ждали %v", step, domain, total, names, want)
		}
		if names, total := lookup(t, dir, domain, 1); total != len(want) || !slices.Equal(names, want[:min(1, len(want))]) {
			t.Errorf("%s, %s с пределом 1: всего %d, имена %v", step, domain, total, names)
		}
	}
}

// listing: имена файлов папки и суммы их содержимого.
func listing(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, fmt.Sprintf("%s %x", e.Name(), sha256.Sum256(data)))
	}
	return strings.Join(lines, "\n")
}

func TestSegmentRoundTrip(t *testing.T) {
	var revs []string
	for i := range 500 {
		revs = append(revs, fmt.Sprintf("com.example.host%04d", i), fmt.Sprintf("com.example.host%04d.www", i))
	}
	path := filepath.Join(t.TempDir(), "one.seg")
	seg, err := buildSegment(path, bytes.NewReader(pack(t, revs...)), "", "", 200)
	if err != nil {
		t.Fatal(err)
	}
	if seg.First != revs[0] || seg.Last != revs[len(revs)-1] || seg.Names != 1000 || seg.Source != 1000 {
		t.Errorf("описание сегмента: %+v", seg)
	}
	f, err := openSegment(path, seg.Last)
	if err != nil {
		t.Fatal(err)
	}
	defer f.close()
	if len(f.blocks) < 50 {
		t.Fatalf("блоков %d, имена должны были разойтись по многим", len(f.blocks))
	}
	var got []string
	for k := range f.blocks {
		names, err := f.read(k)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, names...)
	}
	if !slices.Equal(got, revs) {
		t.Errorf("после записи и чтения имена другие: %d против %d", len(got), len(revs))
	}
	if st, err := os.Stat(path); err != nil || st.Size() != seg.Bytes {
		t.Errorf("размер сегмента %v, в описании %d", st, seg.Bytes)
	}
}

func TestGoodName(t *testing.T) {
	cases := []struct {
		rev  string
		want bool
	}{
		{"com.example", true},
		{"com.example.under_score", true},
		{"com.example.xn--e1afmkfd", true},
		{"com.example.7.113.0.203", true},
		{"org.example." + strings.Repeat("l", 63), true},
		{"com.example.UPPER", false},
		{"192.0.2.1", false},
		{"com.example..empty", false},
		{"singlelabel", false},
		{"com.example." + strings.Repeat("y", 64), false},
		{"com.example.sp ace", false},
		{"com.example.-lead", false},
		{"com.example.trail-", false},
		{"com.example.", false},
		{"test.long" + strings.Repeat("."+strings.Repeat("x", 60), 5), false},
	}
	for _, c := range cases {
		if got := goodName([]byte(c.rev)); got != c.want {
			t.Errorf("goodName(%q) = %v", c.rev, got)
		}
	}
}

// Образец из двух частей: ответы посчитаны отдельно от tim и не зависят от того, как имена легли в блоки.
func TestLookupSample(t *testing.T) {
	var want struct {
		Names   int64
		Source  int64
		Queries map[string]struct {
			Total  int
			First  []string
			SHA256 string
		}
	}
	readJSON(t, "expected-lookup.json", &want)
	parts := [][]byte{readSample(t, sampleParts[0]), readSample(t, sampleParts[1])}
	for _, block := range []int{1, 100, 4096, blockSize} {
		dir := t.TempDir()
		rel := Release{ID: sampleRelease, Nodes: want.Source}
		if err := update(dir, block, rel, len(parts), serve(-1, nil, parts...), nil); err != nil {
			t.Fatal(err)
		}
		base, err := Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		if ready, total := base.Parts(); base.Release() != sampleRelease || base.Names() != want.Names || ready != 2 || total != 2 {
			t.Errorf("блок %d: выпуск %s, имён %d, частей %d из %d", block, base.Release(), base.Names(), ready, total)
		}
		for domain, q := range want.Queries {
			first, total := lookup(t, dir, domain, 20)
			if total != q.Total || !slices.Equal(first, q.First) {
				t.Errorf("блок %d, %s с пределом 20: всего %d, имена %v", block, domain, total, first)
			}
			all, total := lookup(t, dir, domain, 1<<20)
			sum := sha256.Sum256([]byte(strings.Join(all, "\n")))
			if total != q.Total || hex.EncodeToString(sum[:]) != q.SHA256 {
				t.Errorf("блок %d, %s без предела: всего %d, имён %d", block, domain, total, len(all))
			}
		}
	}
}

// "-" меньше ".": между именем домена и его поддоменами стоят чужие имена, и граница блока или сегмента проходит где угодно.
func TestLookupAcrossBlocks(t *testing.T) {
	revs := []string{"com.example", "com.example-shop", "com.example-shop.www", "com.example.api", "com.example.www",
		"com.example.xn--e1afmkfd", "com.examples"}
	cases := []struct {
		domain string
		limit  int
		names  []string
		total  int
	}{
		{"example.com", 10, []string{"example.com", "api.example.com", "www.example.com", "xn--e1afmkfd.example.com"}, 4},
		{"EXAMPLE.com.", 10, []string{"example.com", "api.example.com", "www.example.com", "xn--e1afmkfd.example.com"}, 4},
		{"example-shop.com", 10, []string{"example-shop.com", "www.example-shop.com"}, 2},
		{"examples.com", 10, []string{"examples.com"}, 1},
		{"пример.example.com", 10, []string{"xn--e1afmkfd.example.com"}, 1},
		{"exampl.com", 10, nil, 0},
		{"com", 2, []string{"example.com", "example-shop.com"}, 7},
		{"example.com", 1, []string{"example.com"}, 4},
		{"example.com", 0, nil, 4},
	}
	for split := range len(revs) {
		for _, block := range []int{1, 40, blockSize} {
			dir := t.TempDir()
			if split == 0 {
				install(t, dir, block, sampleRelease, pack(t, revs...))
			} else {
				install(t, dir, block, sampleRelease, pack(t, revs[:split]...), pack(t, revs[split:]...))
			}
			for _, c := range cases {
				names, total := lookup(t, dir, c.domain, c.limit)
				if total != c.total || !slices.Equal(names, c.names) {
					t.Errorf("граница %d, блок %d, %s с пределом %d: всего %d, имена %v", split, block, c.domain, c.limit, total, names)
				}
			}
		}
	}
}

func TestETag(t *testing.T) {
	var want map[string]struct {
		Whole  string `json:"etag_part_8mib"`
		Pieces string `json:"etag_part_4kib"`
	}
	readJSON(t, "expected-etag.json", &want)
	for file, tags := range want {
		data := readSample(t, file)
		// кусок не тот, которым считает tim: ETag не сходится, часть всё равно годится
		for tag, kept := range map[string]string{tags.Whole: tags.Whole, `"` + tags.Whole + `"`: tags.Whole, tags.Pieces: ""} {
			path := filepath.Join(t.TempDir(), "one.seg")
			seg, err := buildSegment(path, bytes.NewReader(data), tag, "", blockSize)
			if err != nil || seg.ETag != kept {
				t.Errorf("%s с ETag %s: сошёлся %q, ошибка %v", file, tag, seg.ETag, err)
			}
		}
	}
	// ответы посчитаны отдельно: md5 от md5 кусков по 8 МиБ и число кусков
	for size, tag := range map[int]string{8<<20 + 1: "a5eb57d36bf73a683183c5a0fb4a37ab-2", 16 << 20: "3a2d20e2e504fe056bbaae5b4c2351fd-2"} {
		sum := newETagSum(tag)
		for rest := size; rest > 0; rest -= min(rest, 1000003) {
			_, _ = sum.Write(make([]byte, min(rest, 1000003)))
		}
		if !sum.matches() {
			t.Errorf("%d нулей: ETag %s не сошёлся", size, tag)
		}
	}
}

func TestParseGraphInfo(t *testing.T) {
	data := readSample(t, "graphinfo.json")
	second := bytes.Index(data, []byte("cc-main-2098"))
	if second < 0 {
		t.Fatal("в образце нет второго выпуска")
	}
	cases := []struct {
		name string
		data []byte
		want Release
	}{
		{"свежий выпуск первый", data, Release{ID: sampleRelease, Nodes: 4498}},
		{"обрыв после первой записи", data[:second], Release{ID: sampleRelease, Nodes: 4498}},
		{"обрыв внутри первой записи", data[:200], Release{}},
		{"пустой список", []byte("[]"), Release{}},
		{"не список", []byte(`{"id": "cc-main-2099-jan-feb-mar"}`), Release{}},
		{"имя с путём", []byte(`[{"id": "../../etc", "stats": {"host": {"nodes": 5}}}]`), Release{}},
		{"нет числа имён", []byte(`[{"id": "cc-main-2099-jan-feb-mar"}]`), Release{}},
	}
	for _, c := range cases {
		got, err := ParseGraphInfo(c.data)
		if got != c.want || (err == nil) != (c.want.ID != "") {
			t.Errorf("%s: %+v, ошибка %v", c.name, got, err)
		}
	}
}

func TestParsePaths(t *testing.T) {
	root := "https://data.commoncrawl.org/projects/hyperlinkgraph/" + sampleRelease + "/host/"
	if got := PathsURL(sampleRelease); got != root+sampleRelease+"-host-vertices.paths.gz" {
		t.Errorf("адрес списка частей: %s", got)
	}
	urls, err := ParsePaths(gz(t, string(readSample(t, "host-vertices.paths"))))
	if err != nil || !slices.Equal(urls, []string{root + "vertices/" + sampleParts[0], root + "vertices/" + sampleParts[1]}) {
		t.Errorf("адреса частей: %v, ошибка %v", urls, err)
	}
	for name, text := range map[string]string{
		"пусто":         "\n",
		"чужой адрес":   "https://example.com/part-00000.txt.gz\n",
		"выход из пути": "projects/hyperlinkgraph/../../part-00000.txt.gz\n",
		"не часть":      "projects/hyperlinkgraph/" + sampleRelease + "/host/index.html\n",
		"пробел в пути": "projects/hyperlinkgraph/x /part-00000.txt.gz\n",
	} {
		if urls, err := ParsePaths(gz(t, text)); err == nil {
			t.Errorf("%s: ошибки нет, адреса %v", name, urls)
		}
	}
	if _, err := ParsePaths([]byte("не gzip")); err == nil {
		t.Error("не gzip: ошибки нет")
	}
}

func readSample(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func readJSON(t *testing.T, name string, v any) {
	t.Helper()
	if err := json.Unmarshal(readSample(t, name), v); err != nil {
		t.Fatal(err)
	}
}
