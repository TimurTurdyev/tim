package namebase

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"golang.org/x/net/idna"

	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

const manifestName = "manifest.json"

// ErrNoBase: архив имён на эту машину не ставили.
var ErrNoBase = errors.New("локального архива имён нет")

// Base: локальный архив имён, каким он был на момент Open.
type Base struct {
	dir string
	m   *manifest
}

// Release: выпуск списка хостов; посреди обновления - тот, что ставится.
func (b *Base) Release() string { return b.m.Release }

// Names: сколько имён в архиве; посреди обновления - с запасом на прежний сегмент, который заменён не весь.
func (b *Base) Names() int64 {
	var names int64
	for _, s := range slices.Concat(b.m.Segments, b.m.Old) {
		names += s.Names
	}
	return names
}

// Updated: когда архив меняли в последний раз.
func (b *Base) Updated() time.Time { return b.m.Updated }

// Parts: сколько частей выпуска стоит и сколько их всего; стоит меньше - обновление не закончено.
func (b *Base) Parts() (ready, total int) {
	return len(b.m.Segments), max(b.m.Parts, len(b.m.Segments))
}

// Lookup: имя domain и его поддомены в порядке имён, прочитанных с конца; не больше limit (limit <= 0 - только счёт), total - сколько их всего.
func (b *Base) Lookup(domain string, limit int) (names []string, total int, err error) {
	q, err := newQuery(domain)
	if err != nil {
		return nil, 0, err
	}
	res, err := b.m.lookup(b.dir, q, limit)
	if errors.Is(err, fs.ErrNotExist) {
		// сегмент убрало обновление, идущее рядом: описание на диске уже другое
		if fresh, lerr := loadManifest(b.dir); lerr == nil {
			res, err = fresh.lookup(b.dir, q, limit)
		}
	}
	if err != nil {
		return nil, 0, fmt.Errorf("поиск в архиве имён: %w", err)
	}
	return res.names, res.total, nil
}

// segment: сегмент в описании архива - одна часть исходника.
type segment struct {
	File   string `json:"file,omitempty"`
	First  string `json:"first,omitempty"`
	Last   string `json:"last,omitempty"`
	Names  int64  `json:"names"`
	Bytes  int64  `json:"bytes,omitempty"`
	Source int64  `json:"source,omitempty"` // строк в части исходника вместе с отсеянными
	ETag   string `json:"etag,omitempty"`   // ETag части, если он сошёлся с прочитанным
	After  string `json:"after,omitempty"`  // у прежнего сегмента: имена не больше этого уже не его
}

func (s segment) lookup(dir string, q query, after string, res *found) error {
	if s.Names == 0 || s.Last < q.rev || s.First >= q.end || s.Last <= after {
		return nil
	}
	f, err := openSegment(filepath.Join(dir, s.File), s.Last)
	if err != nil {
		return err
	}
	defer f.close()
	return f.scan(q, after, res)
}

// manifest: Segments - готовые части выпуска Release. Посреди обновления Old - остаток прежнего архива, из него берутся только имена после Cut.
type manifest struct {
	Release  string    `json:"release"`
	Parts    int       `json:"parts,omitempty"`
	Gen      int       `json:"gen,omitempty"` // номер установки в именах файлов: выпуски не затирают друг друга
	Updated  time.Time `json:"updated,omitzero"`
	Segments []segment `json:"segments"`
	Old      []segment `json:"old,omitempty"`
	Cut      string    `json:"cut,omitempty"`
}

func (m *manifest) save(dir string) error {
	data, err := json.MarshalIndent(m, "", " ")
	if err != nil {
		return err
	}
	return runner.WriteAtomic(filepath.Join(dir, manifestName), append(data, '\n'), 0o644)
}

func (m *manifest) lookup(dir string, q query, limit int) (found, error) {
	res := found{limit: limit}
	for _, s := range m.Segments {
		if err := s.lookup(dir, q, "", &res); err != nil {
			return res, err
		}
	}
	for _, s := range m.Old {
		if err := s.lookup(dir, q, max(s.After, m.Cut), &res); err != nil {
			return res, err
		}
	}
	return res, nil
}

// query: домен задом наперёд и границы его поддоменов. Между rev и pre стоят соседи вида rev-...
type query struct {
	rev string
	pre string // rev + "."
	end string // rev + "/": первый знак после точки
}

type found struct {
	names []string
	total int
	limit int
}

func (r *found) full() bool { return len(r.names) >= r.limit }

func (r *found) add(revs []string) {
	r.total += len(revs)
	room := max(0, min(len(revs), r.limit-len(r.names)))
	for _, rev := range revs[:room] {
		r.names = append(r.names, flip(rev))
	}
}

// Open открывает архив в папке dir; архива нет - ErrNoBase.
func Open(dir string) (*Base, error) {
	m, err := loadManifest(dir)
	if err != nil {
		return nil, err
	}
	return &Base{dir: dir, m: m}, nil
}

func loadManifest(dir string) (*manifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, manifestName))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNoBase
	}
	if err != nil {
		return nil, fmt.Errorf("описание архива имён не читается: %w", err)
	}
	m := &manifest{}
	if err := json.Unmarshal(data, m); err != nil {
		return nil, fmt.Errorf("описание архива имён не разобрано: %w", err)
	}
	for _, s := range slices.Concat(m.Segments, m.Old) {
		// файл из описания открывают и удаляют: он обязан лежать в самой папке архива
		if s.Names > 0 && (!filepath.IsLocal(s.File) || strings.ContainsAny(s.File, `/\`)) {
			return nil, errors.New("описание архива имён повреждено")
		}
	}
	return m, nil
}

func newQuery(domain string) (query, error) {
	// как в браузерах: подчёркивание и дефисы подряд в именах архива есть
	form := idna.New(idna.MapForLookup(), idna.StrictDomainName(false), idna.CheckHyphens(false))
	ascii, err := form.ToASCII(strings.Trim(strings.TrimSpace(domain), "."))
	if err != nil || ascii == "" {
		return query{}, fmt.Errorf("имя домена не разобрано: %s", ui.Clip(ui.Plain(domain), 80))
	}
	rev := flip(strings.ToLower(ascii))
	return query{rev: rev, pre: rev + ".", end: rev + "/"}, nil
}

// flip переставляет метки имени в обратном порядке: www.example.com <-> com.example.www.
func flip(name string) string {
	labels := strings.Split(name, ".")
	slices.Reverse(labels)
	return strings.Join(labels, ".")
}
