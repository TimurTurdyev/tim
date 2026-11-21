package ctlog

import (
	"maps"
	"slices"
	"sort"
	"testing"
	"time"
)

func TestTilePath(t *testing.T) {
	cases := map[int64]string{
		0:          "000",
		181:        "181",
		1000:       "x001/000",
		90216:      "x090/216",
		4733181:    "x004/x733/181",
		1234567890: "x001/x234/x567/890",
	}
	for n, want := range cases {
		if got := tilePath(n); got != want {
			t.Errorf("плитка %d: %s, ждали %s", n, got, want)
		}
	}
	// номер записи из метки образца sct-ext-letsencrypt.hex
	if got := tilePath(1211694457 / tileWidth); got != "x004/x733/181" {
		t.Errorf("плитка записи 1211694457: %s", got)
	}
}

func TestParseTile(t *testing.T) {
	const first = 1795562438120
	want := []string{
		"dc.ipa.example.com", "example.com", "git.example.com", "glpi.ipa.example.com", "mail.example.com",
		"openqa.ipa.example.com", "stage.api.example.com", "www.mail.example.com", "xn--e1afmkfd.example.com",
	}
	// в секунде от записи git.example.com; notexample.com и example.com.evil.example.net - не домен
	near := []string{
		"dc.ipa.example.com", "example.com", "git.example.com", "glpi.ipa.example.com", "mail.example.com",
		"openqa.ipa.example.com", "www.mail.example.com",
	}
	for _, file := range []string{"names-tile.jsonl", "names-tile.jsonl.gz"} {
		body, err := gunzip(sample(t, file))
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		got := parseTile(body, "example.com")
		if got.count != 12 || got.first != first || got.last != first+3999 {
			t.Errorf("%s: записей %d, время %d..%d", file, got.count, got.first, got.last)
		}
		all, beside := map[string]bool{}, map[string]bool{}
		for _, h := range got.hits {
			all[h.name] = true
			if h.at >= first && h.at <= first+2000 {
				beside[h.name] = true
			}
		}
		if names := slices.Sorted(maps.Keys(all)); !slices.Equal(names, want) {
			t.Errorf("%s: имена %q", file, names)
		}
		if names := slices.Sorted(maps.Keys(beside)); !slices.Equal(names, near) {
			t.Errorf("%s: имена рядом %q", file, names)
		}
	}
}

func TestParseTileOddLines(t *testing.T) {
	body := "\n" +
		`{"Subject":{"CommonName":"a.example.com"},"Timestamp":1795562438120,"DNS":["A.example.com"]}` + "\n" +
		"не json\n" +
		`{"Timestamp":1795562439000,"DNS":["b.example.net"]}`
	got := parseTile([]byte(body), "example.com")
	if got.count != 3 || got.first != 1795562438120 || got.last != 1795562439000 {
		t.Errorf("записей %d, время %d..%d", got.count, got.first, got.last)
	}
	if len(got.hits) != 1 || got.hits[0] != (hit{name: "a.example.com", at: 1795562438120}) {
		t.Errorf("имена %+v", got.hits)
	}
}

func TestParseCheckpoint(t *testing.T) {
	size, err := parseCheckpoint(sample(t, "checkpoint.txt"))
	if err != nil || size != 1437394885 {
		t.Errorf("размер %d, ошибка %v", size, err)
	}
	for _, bad := range []string{"", "log.example.com\n12", "log.example.com\nmany\nhash\n", "<html>\n-5\nx\n"} {
		if size, err := parseCheckpoint([]byte(bad)); err == nil {
			t.Errorf("%q: размер %d без ошибки", bad, size)
		}
	}
}

func TestRetryPause(t *testing.T) {
	now := time.Date(2026, 11, 25, 0, 52, 5, 0, time.UTC)
	cases := []struct {
		header string
		try    int
		want   time.Duration
	}{
		{"", 0, 100 * time.Millisecond},
		{"", 3, 800 * time.Millisecond},
		{"", 9, 1600 * time.Millisecond},
		{"2", 0, 2 * time.Second},
		{"0", 2, 400 * time.Millisecond},
		{"3600", 0, 10 * time.Second},
		{"Wed, 25 Nov 2026 00:52:08 GMT", 0, 3 * time.Second},
		// дата с UTC и уже в прошлом: так отвечают журналы на skylight
		{"Wed, 25 Nov 2026 00:52:00 UTC", 1, 200 * time.Millisecond},
		{"Wed, 25 Nov 2026 00:52:07 UTC", 1, 2 * time.Second},
		{"soon", 2, 400 * time.Millisecond},
	}
	for _, c := range cases {
		if got := retryPause(c.header, c.try, now); got != c.want {
			t.Errorf("Retry-After %q, попытка %d: %v, ждали %v", c.header, c.try, got, c.want)
		}
	}
}

// journal: модель журнала "номер записи -> время" вместо сети.
type journal struct {
	size int64
	at   func(pos int64) int64
}

// find гоняет nextStep по модели, как locate по журналу: прочитанная плитка даёт время первой и последней
// записи. Отдаёт итог и число прочитанных плиток.
func (j journal) find(t *testing.T, known []point, at, tol int64) (step, int) {
	t.Helper()
	points := slices.Clone(known)
	last := j.size/tileWidth - 1
	read := map[int64]bool{}
	for n := range maxSteps {
		next := nextStep(points, at, tol, n, last)
		if next.kind != stepRead {
			return next, len(read)
		}
		fresh := false
		for _, number := range next.tiles {
			if number < 0 || number > last {
				t.Fatalf("шаг %d: плитка %d вне журнала", n, number)
			}
			if read[number] {
				continue
			}
			read[number], fresh = true, true
			for _, pos := range []int64{number * tileWidth, number*tileWidth + tileWidth - 1} {
				points = append(points, point{pos: pos, at: j.at(pos)})
			}
		}
		if !fresh {
			t.Fatalf("шаг %d: плитки %v уже прочитаны, поиск стоит", n, next.tiles)
		}
		slices.SortFunc(points, func(a, b point) int { return int(a.pos - b.pos) })
	}
	t.Fatalf("время %d: поиск не кончился за %d шагов", at, maxSteps)
	return step{}, 0
}

// near: плитка стоит у времени at - в ней запись не дальше tol или она на стыке, где at попадает в паузу.
func (j journal) near(number, at, tol int64) bool {
	after := int64(sort.Search(int(j.size), func(pos int) bool { return j.at(int64(pos)) > at }))
	if number == after/tileWidth || number == max(after-1, 0)/tileWidth {
		return true
	}
	return j.at(number*tileWidth) <= at+tol && j.at(number*tileWidth+tileWidth-1) >= at-tol
}

func TestNextStep(t *testing.T) {
	const (
		start = int64(1795000000000)
		tol   = 30_000
	)
	// время пачками по секунде, как в журналах
	steady := journal{size: 20_000 * tileWidth, at: func(pos int64) int64 { return start + pos/100*1000 }}
	// поток вырос в 12 раз: первая половина записей шла по 20 в секунду, вторая по 250
	grown := journal{size: 20_000 * tileWidth, at: func(pos int64) int64 {
		if pos < 2_500_000 {
			return start + pos/20*1000
		}
		return start + 125_000_000 + (pos-2_500_000)/250*1000
	}}
	// час тишины посреди журнала
	paused := journal{size: 4_000 * tileWidth, at: func(pos int64) int64 {
		if pos < 400_000 {
			return start + pos/100*1000
		}
		return start + 4_000_000 + 3_600_000 + (pos-400_000)/100*1000
	}}
	mark := func(j journal, pos int64) point { return point{pos: pos, at: j.at(pos)} }
	cases := []struct {
		name    string
		journal journal
		known   []point
		at      int64
		kind    stepKind
		reads   int // не больше стольких плиток
	}{
		{"ровный поток", steady, nil, start + 31_234_567, stepFound, 5},
		{"у начала", steady, nil, start + 1500, stepFound, 3},
		{"раньше начала", steady, nil, start - 1, stepNone, 2},
		{"позже конца", steady, nil, start + 60_000_000, stepFound, 2},
		{"выросший поток", grown, nil, start + 100_000_000, stepFound, 10},
		{"выросший поток, быстрая часть", grown, nil, start + 130_000_000, stepFound, 8},
		{"в паузе", paused, nil, start + 5_000_000, stepFound, 12},
		{"метки рядом", steady, []point{mark(steady, 3_000_000), mark(steady, 3_090_000)}, steady.at(3_050_000), stepFound, 2},
		{"метка в пределах допуска", steady, []point{mark(steady, 3_000_000)}, steady.at(3_000_000) + 20_000, stepFound, 1},
		{"метки с одной стороны", steady, []point{mark(steady, 1_000_000), mark(steady, 1_000_300)}, steady.at(4_200_000), stepFound, 5},
	}
	for _, c := range cases {
		got, reads := c.journal.find(t, c.known, c.at, tol)
		switch {
		case got.kind != c.kind:
			t.Errorf("%s: итог %d, ждали %d", c.name, got.kind, c.kind)
		case got.kind == stepFound && !c.journal.near(got.tiles[0], c.at, tol):
			t.Errorf("%s: плитка %d не у времени %d", c.name, got.tiles[0], c.at)
		case reads > c.reads:
			t.Errorf("%s: прочитано плиток %d, ждали не больше %d", c.name, reads, c.reads)
		}
	}
}
