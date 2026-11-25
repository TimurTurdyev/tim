package namebase

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"testing"
)

// Новый выпуск встаёт по частям поверх прежнего: до границы поиск отдаёт новые имена, после неё прежние, обрыв продолжается с неготовой части.
func TestUpdateMidway(t *testing.T) {
	old := []string{"com.example", "com.example.api", "com.example.old", "com.example.www", "net.example",
		"net.example.mail", "net.example.www", "org.example", "org.example.docs", "org.example.www", "org.example.zzz"}
	fresh := []string{"com.example", "com.example.api", "com.example.new", "com.example.www", "net.example",
		"net.example.www", "net.example.www2", "org.example", "org.example.docs"}
	parts := [][]byte{pack(t, fresh[:5]...), pack(t, fresh[5:]...)}
	rel := Release{ID: nextRelease, Nodes: int64(len(fresh))}
	dir := t.TempDir()
	install(t, dir, 16, sampleRelease, pack(t, old[:3]...), pack(t, old[3:8]...), pack(t, old[8:]...))

	check := func(step string, revs []string, files, pending int) {
		t.Helper()
		same(t, dir, step, revs, "example.com", "example.net", "example.org", "com", "net", "org")
		if entries, _ := os.ReadDir(dir); len(entries) != files+1 {
			t.Errorf("%s: в папке %d файлов, ждали описание и %d сегментов", step, len(entries), files)
		}
		if n := Pending(dir, rel); n != pending {
			t.Errorf("%s: Pending = %d", step, n)
		}
	}

	if err := update(dir, 16, rel, 2, serve(0, nil, parts...), nil); !errors.Is(err, errCut) {
		t.Fatalf("обрыв на первой части: %v", err)
	}
	check("до первой части", old, 3, 0)

	if err := update(dir, 16, rel, 2, serve(1, nil, parts...), nil); !errors.Is(err, errCut) {
		t.Fatalf("обрыв на второй части: %v", err)
	}
	// первая часть кончается на net.example: прежние сегменты до этого имени убраны, от второго остался хвост
	check("после первой части", slices.Concat(fresh[:5], old[5:]), 3, 1)

	var asked, steps []int
	err := update(dir, 16, rel, 2, serve(-1, &asked, parts...), func(done, total int) { steps = append(steps, done, total) })
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(asked, []int{1}) || !slices.Equal(steps, []int{2, 2}) {
		t.Errorf("после обрыва запрошены части %v, ход %v", asked, steps)
	}
	check("после второй части", fresh, 2, 2)
}

// Прежний архив сам стоял наполовину: его остаток помнит свою границу, и третий выпуск не возвращает имена, которые второй уже заменил.
func TestUpdateChain(t *testing.T) {
	first := []string{"com.example.a1", "com.example.a2", "net.example.a1", "net.example.a2", "org.example.a1", "org.example.a2"}
	second := []string{"com.example.b1", "net.example.b1", "net.example.b2", "org.example.b1"}
	third := []string{"com.example.c1", "net.example.c1", "org.example.c1"}
	zones := []string{"com", "net", "org"}
	dir := t.TempDir()
	install(t, dir, 16, "cc-main-2099-a", pack(t, first[:2]...), pack(t, first[2:5]...), pack(t, first[5:]...))

	if err := update(dir, 16, Release{ID: "cc-main-2099-b"}, 2, serve(1, nil, pack(t, second[:2]...)), nil); !errors.Is(err, errCut) {
		t.Fatal(err)
	}
	same(t, dir, "второй выпуск наполовину", []string{second[0], second[1], first[4], first[5]}, zones...)

	last := Release{ID: "cc-main-2099-c", Nodes: int64(len(third))}
	parts := [][]byte{pack(t, third[:1]...), pack(t, third[1:]...)}
	if err := update(dir, 16, last, 2, serve(1, nil, parts...), nil); !errors.Is(err, errCut) {
		t.Fatal(err)
	}
	same(t, dir, "третий выпуск наполовину", []string{third[0], second[1], first[4], first[5]}, zones...)

	if err := update(dir, 16, last, 2, serve(-1, nil, parts...), nil); err != nil {
		t.Fatal(err)
	}
	same(t, dir, "третий выпуск целиком", third, zones...)
	if entries, _ := os.ReadDir(dir); len(entries) != 3 {
		t.Errorf("в папке %d файлов, ждали описание и 2 сегмента", len(entries))
	}
}

// Негодная часть - ошибка, а папка остаётся как была. При обрыве gzip последняя строка приходит обрезанной: за нарушение порядка она сойти не должна.
func TestUpdateBadPart(t *testing.T) {
	type badPart struct {
		name  string
		part  []byte
		cause error // nil - годится любая ошибка
		alone bool  // часть негодна и сама по себе, первой в пустой папке
	}
	good := pack(t, "com.example", "com.example.www")
	var revs []string
	for i := range 2000 {
		revs = append(revs, fmt.Sprintf("net.example.host%04d", i))
	}
	whole := pack(t, revs...)
	flipped := slices.Clone(whole)
	flipped[len(flipped)-8] ^= 0xff
	cases := []badPart{
		{"не сошлась сумма gzip", flipped, gzip.ErrChecksum, true},
		{"имена не по порядку", pack(t, "net.example.www", "net.example"), nil, true},
		{"имя не после прошлой части", pack(t, "com.example.www", "net.example"), nil, false},
		{"строка без табуляции", gz(t, "0\tnet.example\nnet.example.www\n"), nil, true},
		{"не gzip", []byte("net.example\n"), nil, true},
	}
	// обрыв в разных местах: где-то строка рвётся посередине, где-то пропадает только хвост с суммой
	for _, cut := range []int{len(whole) / 3, len(whole) / 2, len(whole) - 100, len(whole) - 4} {
		cases = append(cases, badPart{fmt.Sprintf("обрыв gzip на %d из %d", cut, len(whole)), whole[:cut], io.ErrUnexpectedEOF, true})
	}
	for _, c := range cases {
		dir := t.TempDir()
		rel := Release{ID: sampleRelease}
		if err := update(dir, 16, rel, 2, serve(1, nil, good), nil); !errors.Is(err, errCut) {
			t.Fatal(err)
		}
		before := listing(t, dir)
		err := update(dir, 16, rel, 2, serve(-1, nil, good, c.part), nil)
		if err == nil || (c.cause != nil && !errors.Is(err, c.cause)) {
			t.Errorf("%s: ошибка %v", c.name, err)
		}
		if after := listing(t, dir); after != before {
			t.Errorf("%s: папка изменилась:\n%s\nбыло:\n%s", c.name, after, before)
		}
		if names, total := lookup(t, dir, "example.com", 10); total != 2 || len(names) != 2 {
			t.Errorf("%s: поиск после ошибки: всего %d, имена %v", c.name, total, names)
		}
		if !c.alone {
			continue
		}
		empty := t.TempDir()
		if err := update(empty, 16, rel, 1, serve(-1, nil, c.part), nil); err == nil {
			t.Errorf("%s в пустой папке: ошибки нет", c.name)
		}
		if entries, _ := os.ReadDir(empty); len(entries) != 0 {
			t.Errorf("%s в пустой папке: осталось файлов %d", c.name, len(entries))
		}
	}
}

// Отсеянные имена в архив не идут, но в сверке с числом имён выпуска участвуют.
func TestUpdateTotal(t *testing.T) {
	part := pack(t, "192.0.2.1", "com.example", "com.example.UPPER", "com.example.www", "singlelabel")
	dir := t.TempDir()
	if err := update(dir, 16, Release{ID: sampleRelease, Nodes: 5}, 1, serve(-1, nil, part), nil); err != nil {
		t.Fatal(err)
	}
	base, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if base.Names() != 2 {
		t.Errorf("имён в архиве %d", base.Names())
	}
	if err := update(dir, 16, Release{ID: sampleRelease, Nodes: 6}, 1, serve(-1, nil, part), nil); err == nil {
		t.Error("в частях имён меньше, чем обещал выпуск, а ошибки нет")
	}
	if _, total := lookup(t, dir, "example.com", 10); total != 2 {
		t.Errorf("после расхождения в числе имён поиск отдал %d", total)
	}
	// Регрессия: выпуск объявил на одно имя меньше, чем лежит в его частях, и полный архив считался сбоем.
	var many []string
	for i := range 2000 {
		many = append(many, fmt.Sprintf("com.example.h%04d", i))
	}
	if err := update(t.TempDir(), 16, Release{ID: sampleRelease, Nodes: 1999}, 1, serve(-1, nil, pack(t, many...)), nil); err != nil {
		t.Errorf("расхождение на одно имя из двух тысяч: %v", err)
	}
}
