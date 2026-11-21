package ctlog

import (
	"cmp"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/TimurTurdyev/tim/internal/cache"
	"github.com/TimurTurdyev/tim/internal/par"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/ui"
)

const (
	listURL     = "https://www.gstatic.com/ct/log_list/v3/all_logs_list.json"
	listTimeout = 6 * time.Second
	// day: столько живут список журналов и ответы проб.
	day = 24 * time.Hour
	// letsEncrypt: оператор, чьи журналы читаются и там, где метки сертификата нет.
	letsEncrypt = "Let's Encrypt"
	// Ключи кэша начинаются с "_": рядом лежат выпуски по доменам, а домен так не начинается.
	listKey  = "_loglist"
	namesKey = "_names"
)

// Копия списка на случай, когда он недоступен и кэша ещё нет: файл как у источника, только сжат.
//
//go:embed all_logs_list.json.gz
var builtinList []byte

// logInfo: плиточный журнал из списка.
type logInfo struct {
	ID       string    `json:"id"`
	Operator string    `json:"operator"`
	Name     string    `json:"name"`
	URL      string    `json:"url"` // адрес для чтения, с "/" на конце
	State    string    `json:"state,omitempty"`
	Type     string    `json:"type,omitempty"` // test, monitoring_only; у боевых пусто
	Start    time.Time `json:"start"`
	End      time.Time `json:"end"`
}

// accepts: сегмент боевой и принимает сертификаты с таким сроком; по названию не узнать, границы у всех свои.
func (l logInfo) accepts(notAfter time.Time) bool {
	if l.Type != "" || l.State == "rejected" || l.State == "pending" {
		return false
	}
	return !notAfter.Before(l.Start) && notAfter.Before(l.End)
}

type namesProbe struct {
	Has bool      `json:"has"`
	At  time.Time `json:"at"`
}

// namesMemo: у каких журналов есть плитки имён - узнаётся только пробой, ответ живёт сутки.
type namesMemo struct {
	mu      sync.Mutex
	path    string
	probes  map[string]namesProbe
	changed bool
}

func loadNamesMemo() *namesMemo {
	m := &namesMemo{path: cache.Path(paths.State("ctlog"), namesKey), probes: map[string]namesProbe{}}
	if e, ok := cache.Load[map[string]namesProbe](m.path); ok {
		for id, p := range e.Value {
			if time.Since(p.At) < day {
				m.probes[id] = p
			}
		}
	}
	return m
}

func (m *namesMemo) get(id string) (has, known bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, known := m.probes[id]
	return p.Has, known
}

func (m *namesMemo) set(id string, has bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.probes[id], m.changed = namesProbe{Has: has, At: time.Now()}, true
}

func (m *namesMemo) save() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.changed {
		_ = cache.Store(m.path, m.probes, 0o644)
	}
}

type tileCall struct {
	done chan struct{}
	loaded
	dropped bool // запрос оборван сроком того, кто спрашивал
}

type loaded struct {
	tile tile
	err  error
}

// point: известное время записи журнала.
type point struct{ pos, at int64 }

// reader: один журнал на время поиска - его размер, прочитанные плитки и пары "номер записи - время".
type reader struct {
	log    logInfo
	web    *web
	gate   *gate
	domain string
	last   int64 // последняя полная плитка; -1 - журнал не открыт или пуст
	names  bool  // плитки имён есть
	mu     sync.Mutex
	calls  map[int64]*tileCall
	points []point // по возрастанию номера записи
}

func newReader(w *web, l logInfo, domain string) *reader {
	parallel := otherParallel
	if l.Operator == letsEncrypt {
		parallel = leParallel
	}
	return &reader{log: l, web: w, gate: w.gate(l.URL, parallel), domain: domain, last: -1, calls: map[int64]*tileCall{}}
}

// open: размер журнала и есть ли плитки имён. Неполная плитка в хвосте не читается: её ширина устаревает за секунду.
func (r *reader) open(ctx context.Context, memo *namesMemo) error {
	has, known := memo.get(r.log.ID)
	if known && !has {
		return nil
	}
	body, status, err := r.web.get(ctx, r.gate, r.log.URL+"checkpoint")
	if err == nil && status != http.StatusOK {
		err = fmt.Errorf("ответ %d", status)
	}
	if err != nil {
		return fmt.Errorf("контрольная точка журнала %s: %w", r.log.Name, err)
	}
	size, err := parseCheckpoint(body)
	if err != nil {
		return fmt.Errorf("журнал %s: %w", r.log.Name, err)
	}
	if r.last = size/tileWidth - 1; r.last < 0 {
		return nil
	}
	if !known {
		_, err = r.tile(ctx, r.last)
		if err != nil && !errors.Is(err, errNoTile) {
			return err
		}
		has = err == nil
		memo.set(r.log.ID, has)
	}
	r.names = has
	return nil
}

// tile: плитка n. Второй запрос той же плитки ждёт первого: плитка в полёте ещё не прочитана.
func (r *reader) tile(ctx context.Context, n int64) (tile, error) {
	for {
		call, mine := r.claim(n)
		if mine {
			return r.fill(ctx, n, call)
		}
		select {
		case <-call.done:
		case <-ctx.Done():
			return tile{}, ctx.Err()
		}
		if !call.dropped {
			return call.tile, call.err
		}
	}
}

// claim: запрос плитки n - идущий или новый; mine - выполнять его нам.
func (r *reader) claim(n int64) (call *tileCall, mine bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if call = r.calls[n]; call != nil {
		return call, false
	}
	call = &tileCall{done: make(chan struct{})}
	r.calls[n] = call
	return call, true
}

func (r *reader) fill(ctx context.Context, n int64, call *tileCall) (tile, error) {
	defer close(call.done)
	call.tile, call.err = r.load(ctx, n)
	switch {
	case call.err == nil:
		r.know(n*tileWidth, call.tile.first)
		r.know(n*tileWidth+int64(call.tile.count)-1, call.tile.last)
	case ctx.Err() != nil:
		// оборвал срок, а не журнал: кто ждал или спросит позже, запросит плитку сам
		call.dropped = true
		r.mu.Lock()
		delete(r.calls, n)
		r.mu.Unlock()
	}
	return call.tile, call.err
}

// several читает плитки разом; ответы в порядке номеров.
func (r *reader) several(ctx context.Context, numbers []int64) []loaded {
	return par.Map(numbers, len(numbers), func(n int64) loaded {
		t, err := r.tile(ctx, n)
		return loaded{tile: t, err: err}
	})
}

func (r *reader) load(ctx context.Context, n int64) (tile, error) {
	body, status, err := r.web.get(ctx, r.gate, r.log.URL+"tile/names/"+tilePath(n))
	switch {
	case err != nil:
		return tile{}, fmt.Errorf("плитка имён журнала %s: %w", r.log.Name, err)
	case status == http.StatusBadRequest, status == http.StatusForbidden, status == http.StatusNotFound:
		return tile{}, errNoTile
	case status != http.StatusOK:
		return tile{}, fmt.Errorf("плитка имён журнала %s: ответ %d", r.log.Name, status)
	}
	t := parseTile(body, r.domain)
	if t.first == 0 {
		return tile{}, fmt.Errorf("плитка имён журнала %s: записей нет", r.log.Name)
	}
	r.web.tiles.Add(1)
	return t, nil
}

// know запоминает время записи: по таким парам ищется место другого времени.
func (r *reader) know(pos, at int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	i, found := slices.BinarySearchFunc(r.points, pos, func(p point, pos int64) int { return cmp.Compare(p.pos, pos) })
	if !found {
		r.points = slices.Insert(r.points, i, point{pos: pos, at: at})
	}
}

// locate: плитка, рядом с которой лежит время at; tol - на сколько мс известная запись может от него отстоять.
func (r *reader) locate(ctx context.Context, at, tol int64) (int64, error) {
	for n := range maxSteps {
		r.mu.Lock()
		next := nextStep(r.points, at, tol, n, r.last)
		r.mu.Unlock()
		switch next.kind {
		case stepFound:
			return next.tiles[0], nil
		case stepNone:
			return 0, errNotInLog
		}
		for _, l := range r.several(ctx, next.tiles) {
			if l.err != nil {
				return 0, l.err
			}
		}
	}
	return 0, errNotInLog
}

// loadLogs: список журналов - кэш на сутки; источник недоступен - прежний кэш, нет и его - встроенная копия.
func loadLogs(ctx context.Context, w *web) (list []logInfo, builtin bool) {
	list, err := cache.Get(cache.Path(paths.State("ctlog"), listKey), day, func() ([]logInfo, error) {
		ctx, cancel := context.WithTimeout(ctx, listTimeout)
		defer cancel()
		body, status, err := w.get(ctx, w.gate(listURL, otherParallel), listURL)
		if err != nil {
			return nil, err
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("список журналов сертификатов: ответ %d", status)
		}
		return parseLogList(body)
	})
	if err == nil && len(list) > 0 {
		return list, false
	}
	data, err := gunzip(builtinList)
	if err != nil {
		return nil, true
	}
	list, _ = parseLogList(data)
	return list, true
}

// parseLogList: плиточные журналы из all_logs_list.json; классические плиток имён не отдают.
func parseLogList(data []byte) ([]logInfo, error) {
	type rawLog struct {
		Description string                     `json:"description"`
		LogID       string                     `json:"log_id"`
		Monitoring  string                     `json:"monitoring_url"`
		State       map[string]json.RawMessage `json:"state"`
		Type        string                     `json:"log_type"`
		Interval    struct {
			Start time.Time `json:"start_inclusive"`
			End   time.Time `json:"end_exclusive"`
		} `json:"temporal_interval"`
	}
	var doc struct {
		Operators []struct {
			Name  string   `json:"name"`
			Tiled []rawLog `json:"tiled_logs"`
		} `json:"operators"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("список журналов сертификатов: %w", err)
	}
	var out []logInfo
	for _, op := range doc.Operators {
		for _, raw := range op.Tiled {
			if raw.LogID == "" || !strings.HasPrefix(raw.Monitoring, "https://") {
				continue
			}
			l := logInfo{
				ID: raw.LogID, Operator: ui.Plain(op.Name), Name: ui.Plain(raw.Description),
				URL: strings.TrimSuffix(raw.Monitoring, "/") + "/", Type: raw.Type,
				Start: raw.Interval.Start, End: raw.Interval.End,
			}
			for state := range raw.State {
				l.State = state
			}
			out = append(out, l)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("в списке журналов сертификатов нет плиточных")
	}
	return out, nil
}
