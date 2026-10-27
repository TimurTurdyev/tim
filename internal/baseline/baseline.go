// Package baseline: что на объекте уже своё - ключи ssh, порты, юниты, записи DNS, автозапуск Mac.
// Первое наблюдение всё запоминает своим. Дальше новое держится, пока его не отметят своим, а не до
// следующего обхода: иначе ночной обход съест тревогу раньше, чем её увидят. Одно хранилище на сервер,
// домен и Mac.
package baseline

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/TimurTurdyev/tim/internal/cache"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// mu: обход проверяет объекты параллельно, а файл у объекта один.
var mu sync.Mutex

// Item: что-то на объекте. Key - по нему сравнивается, Text - как показать.
type Item struct {
	Kind  string    `json:"kind"`
	Key   string    `json:"key"`
	Text  string    `json:"text,omitempty"`
	Prev  string    `json:"prev,omitempty"` // что было на этом месте: у записи DNS - прежнее значение
	Seen  time.Time `json:"seen"`           // когда замечено новым
	After time.Time `json:"after,omitzero"` // прошлое наблюдение: появилось между After и Seen
}

// Snapshot: что есть на объекте сейчас, по видам. Вида нет в снимке - его не удалось прочитать,
// и он не трогается.
type Snapshot map[string][]Item

// Result: итог наблюдения.
type Result struct {
	New   []Item         // всё неотмеченное новое
	First map[string]int // виды, запомненные сейчас впервые, и сколько в них
}

type state struct {
	Known map[string][]string `json:"known"`
	New   []Item              `json:"new,omitempty"`
	Last  time.Time           `json:"last"`
	// Prev: прошлый снимок по видам - из него "было" у нового значения (запись DNS сменилась).
	Prev map[string][]string `json:"prev,omitempty"`
}

func path(object string) string { return cache.Path(paths.State("baseline"), object) }

func load(object string) state {
	e, _ := cache.Load[state](path(object))
	s := e.Value
	if s.Known == nil {
		s.Known = map[string][]string{}
	}
	if s.Prev == nil {
		s.Prev = map[string][]string{}
	}
	return s
}

func store(object string, s state) error {
	if runner.DryRun {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path(object)), 0o755); err != nil {
		return err
	}
	return cache.Store(path(object), s, 0o600)
}

// Observe сравнивает снимок со своим и запоминает. Пропавшее из нового уходит само; своё не забывается,
// чтобы закрытый и снова открытый порт не стал тревогой.
func Observe(object string, snap Snapshot, now time.Time) Result {
	mu.Lock()
	defer mu.Unlock()
	s := load(object)
	res := Result{First: map[string]int{}}
	for kind, items := range snap {
		present := map[string]bool{}
		for _, it := range items {
			present[it.Key] = true
		}
		keys := make([]string, 0, len(items))
		for _, it := range items {
			keys = append(keys, it.Key)
		}
		prev := s.Prev[kind]
		s.Prev[kind] = keys
		known, seen := s.Known[kind]
		if !seen {
			s.Known[kind] = keys
			res.First[kind] = len(items)
			continue
		}
		var gone []string
		for _, k := range prev {
			if !present[k] {
				gone = append(gone, k)
			}
		}
		s.New = slices.DeleteFunc(s.New, func(it Item) bool { return it.Kind == kind && !present[it.Key] })
		for _, it := range items {
			if slices.Contains(known, it.Key) || slices.ContainsFunc(s.New, func(n Item) bool { return n.Kind == kind && n.Key == it.Key }) {
				continue
			}
			it.Kind, it.Seen, it.After = kind, now, s.Last
			if it.Prev == "" {
				it.Prev = strings.Join(gone, ", ")
			}
			s.New = append(s.New, it)
		}
	}
	s.Last = now
	_ = store(object, s)
	res.New = slices.Clone(s.New)
	return res
}

// Pending: неотмеченное новое без наблюдения - для главного экрана и уведомлений.
func Pending(object string) []Item {
	mu.Lock()
	defer mu.Unlock()
	return load(object).New
}

// Accept отмечает своим: kind и key пустые - всё новое объекта.
func Accept(object, kind, key string) error {
	mu.Lock()
	defer mu.Unlock()
	s := load(object)
	s.New = slices.DeleteFunc(s.New, func(it Item) bool {
		if (kind == "" || it.Kind == kind) && (key == "" || it.Key == key) {
			s.Known[it.Kind] = append(s.Known[it.Kind], it.Key)
			return true
		}
		return false
	})
	return store(object, s)
}

// Since: с какого наблюдения считается новое - самое раннее After среди нового.
func Since(items []Item) time.Time {
	var t time.Time
	for _, it := range items {
		if t.IsZero() || !it.After.IsZero() && it.After.Before(t) {
			t = it.After
		}
	}
	return t
}

// FirstText: "запомнил 14 ключей, 9 портов" - что запомнено при первом наблюдении; names - вид -> слова.
func FirstText(first map[string]int, names map[string][3]string, order []string) string {
	var parts []string
	for _, k := range order {
		if n, ok := first[k]; ok && n > 0 {
			w := names[k]
			parts = append(parts, strconv.Itoa(n)+" "+ui.Plural(n, w[0], w[1], w[2]))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "запомнил " + strings.Join(parts, ", ") + " - дальше покажу только новое"
}
