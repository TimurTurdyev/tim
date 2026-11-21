package ctlog

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/TimurTurdyev/tim/internal/par"
)

const (
	defaultBudget   = 30 * time.Second
	defaultMaxBytes = 50 << 20
	// Окно: минута от метки в обе стороны; чужое имя домена в окне двигает край на полторы минуты, предел - десять.
	windowMs    = 60_000
	growMs      = 90_000
	maxWindowMs = 600_000
	// mergeMs: окна одного журнала ближе секунды читаются один раз.
	mergeMs  = 1000
	maxTiles = 600
	// ring: плиток за раз в одну сторону, чтобы срок и объём резали дальние плитки, а не ближние.
	ring = 48
	// Чужие журналы без контакта в User-Agent отдают 2-10 плиток в секунду: на окно не больше otherLimit.
	otherLimit   = 5 * time.Second
	leWindows    = 6
	otherWindows = 2
)

// NearOptions: пределы поиска; нули - 30 с и 50 МБ.
type NearOptions struct {
	Budget   time.Duration
	MaxBytes int64        // объём по сети
	Note     func(string) // строка о ходе работы
}

// NearResult: что нашлось рядом с метками.
type NearResult struct {
	Names []string // имена домена и поддоменов, кроме имён самих сертификатов
	Tiles int      // скачано плиток
	Bytes int64    // принято по сети
	Cut   bool     // вышел срок или предел объёма: прочитано не всё
	Err   error    // журналы недоступны
}

// certMarks: метки одного сертификата.
type certMarks struct {
	at       int64 // самая ранняя метка, мс
	notAfter time.Time
	names    map[string]bool
	marks    []Anchor
}

// task: одно окно - журнал и место в нём: номер записи или только время.
type task struct {
	r     *reader
	at    int64 // мс
	index int64 // -1 - место ищется по времени
	self  map[string]bool
	limit time.Duration // срок на окно; 0 - только общий
}

// search: один вызов Near - открытые журналы и что в них нашлось.
type search struct {
	web     *web
	domain  string
	list    []logInfo
	memo    *namesMemo
	readers map[string]*reader
	mu      sync.Mutex
	found   map[string]bool
	done    int   // окон прочитано
	err     error // первый отказ журнала
}

// prepare: окна для чтения - журналы открыты, окна в журналах без плиток имён отброшены, близкие склеены.
func (s *search) prepare(ctx context.Context, certs []*certMarks) []task {
	tasks := s.plan(certs)
	var wg sync.WaitGroup
	for _, r := range s.readers {
		wg.Go(func() {
			if err := r.open(ctx, s.memo); err != nil {
				s.finish(nil, err)
			}
		})
	}
	wg.Wait()
	tasks = slices.DeleteFunc(tasks, func(t task) bool { return !t.r.names })
	for _, t := range tasks {
		if t.index >= 0 && t.index/tileWidth <= t.r.last {
			t.r.know(t.index, t.at)
		}
	}
	return merge(tasks)
}

// plan: журналы Let's Encrypt под срок сертификата - по номеру записи или времени метки, чужие - только свою метку.
func (s *search) plan(certs []*certMarks) []task {
	var tasks []task
	for _, c := range certs {
		own := map[string]bool{}
		for _, m := range c.marks {
			i := slices.IndexFunc(s.list, func(l logInfo) bool { return l.ID == m.LogID })
			if i < 0 || m.Index < 0 {
				continue
			}
			t := task{r: s.reader(s.list[i]), at: m.Time.UnixMilli(), index: m.Index, self: c.names}
			if s.list[i].Operator != letsEncrypt {
				t.limit = otherLimit
			}
			tasks = append(tasks, t)
			own[m.LogID] = true
		}
		for _, l := range s.list {
			if l.Operator == letsEncrypt && l.accepts(c.notAfter) && !own[l.ID] {
				tasks = append(tasks, task{r: s.reader(l), at: c.at, index: -1, self: c.names})
			}
		}
	}
	return tasks
}

func (s *search) reader(l logInfo) *reader {
	r := s.readers[l.ID]
	if r == nil {
		r = newReader(s.web, l, s.domain)
		s.readers[l.ID] = r
	}
	return r
}

// readAll: у каждого хоста своя очередь окон, чтобы медленный журнал не держал быстрые.
func (s *search) readAll(ctx context.Context, tasks []task) {
	queues := map[*gate][]task{}
	for _, t := range tasks {
		queues[t.r.gate] = append(queues[t.r.gate], t)
	}
	var wg sync.WaitGroup
	for _, queue := range queues {
		wg.Go(func() {
			windows := otherWindows
			if queue[0].r.log.Operator == letsEncrypt {
				windows = leWindows
			}
			group := par.NewGroup(windows)
			for _, t := range queue {
				if ctx.Err() != nil {
					break
				}
				group.Go(func() { s.read(ctx, t) })
			}
			group.Wait()
		})
	}
	wg.Wait()
}

func (s *search) read(ctx context.Context, t task) {
	if t.limit > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, t.limit)
		defer cancel()
	}
	names, err := scan(ctx, t)
	if errors.Is(err, errNotInLog) {
		err = nil
	}
	s.finish(names, err)
}

// finish: итог одного окна или открытия журнала.
func (s *search) finish(names []string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case err == nil:
		s.done++
	case s.err == nil:
		s.err = err
	}
	for _, name := range names {
		s.found[name] = true
	}
}

func (s *search) result(ctx context.Context, certs []*certMarks) NearResult {
	res := NearResult{Tiles: int(s.web.tiles.Load()), Bytes: s.web.bytes.Load(), Cut: ctx.Err() != nil}
	for name := range s.found {
		if !slices.ContainsFunc(certs, func(c *certMarks) bool { return c.names[name] }) {
			res.Names = append(res.Names, name)
		}
	}
	slices.Sort(res.Names)
	// ни одно окно не прочитано; вышедший срок - отказ журналов, только если от них не пришло ни плитки
	if s.done == 0 && s.err != nil && (!res.Cut || res.Tiles == 0) {
		res.Err = fmt.Errorf("журналы сертификатов недоступны: %w", s.err)
	}
	return res
}

// window: окно вокруг метки в одном журнале - прочитанные плитки и края по времени.
type window struct {
	task
	got      map[int64]tile
	lo, hi   int64 // края прочитанного, номера плиток
	from, to int64 // края окна, мс
}

// need: сколько плиток дочитать слева и справа, чтобы закрыть окно; нули - окно закрыто.
func (w *window) need() (left, right int64) {
	first, last := w.got[w.lo], w.got[w.hi]
	perTile := max((last.last-first.first)/(w.hi-w.lo+1), 1)
	if first.first > w.from {
		left = min((first.first-w.from)*11/10/perTile+1, ring, w.lo)
	}
	if last.last < w.to {
		right = min((w.to-last.last)*11/10/perTile+1, ring, w.r.last-w.hi)
	}
	return left, right
}

// extend дочитывает плитки по краям; false - ни один край не сдвинулся.
func (w *window) extend(ctx context.Context, left, right int64) bool {
	lo, hi := w.lo-left, w.hi+right
	_ = w.fetch(ctx, lo, hi)
	// неполученная плитка на краю: дальше в эту сторону окно не идёт
	for ; lo < w.lo; lo++ {
		if _, ok := w.got[lo]; ok {
			break
		}
	}
	for ; hi > w.hi; hi-- {
		if _, ok := w.got[hi]; ok {
			break
		}
	}
	moved := lo < w.lo || hi > w.hi
	w.lo, w.hi = lo, hi
	return moved
}

// fetch читает ещё не прочитанные плитки lo..hi разом; отдаёт первую ошибку.
func (w *window) fetch(ctx context.Context, lo, hi int64) error {
	var want []int64
	for n := lo; n <= hi; n++ {
		if _, ok := w.got[n]; !ok {
			want = append(want, n)
		}
	}
	var first error
	for i, l := range w.r.several(ctx, want) {
		switch {
		case l.err == nil:
			w.got[want[i]] = l.tile
		case first == nil:
			first = l.err
		}
	}
	return first
}

// grow отодвигает края окна от имён домена, которых нет в самом сертификате; false - окно не изменилось.
func (w *window) grow() bool {
	from, to := w.from, w.to
	for n := w.lo; n <= w.hi; n++ {
		for _, h := range w.got[n].hits {
			if h.at >= w.from && h.at <= w.to && !w.self[h.name] {
				from, to = min(from, h.at-growMs), max(to, h.at+growMs)
			}
		}
	}
	from, to = max(from, w.at-maxWindowMs), min(to, w.at+maxWindowMs)
	moved := from != w.from || to != w.to
	w.from, w.to = from, to
	return moved
}

func (w *window) names() []string {
	var out []string
	for n := w.lo; n <= w.hi; n++ {
		for _, h := range w.got[n].hits {
			if h.at >= w.from && h.at <= w.to {
				out = append(out, h.name)
			}
		}
	}
	return out
}

// Near: имена домена, записанные в журналы рядом по времени с anchors: сертификаты сервера выпускаются пачкой.
func Near(ctx context.Context, domain string, anchors []Anchor, opt NearOptions) NearResult {
	domain = hostName(domain)
	certs := groupCerts(anchors)
	if domain == "" || len(certs) == 0 {
		return NearResult{}
	}
	if opt.Budget <= 0 {
		opt.Budget = defaultBudget
	}
	if opt.MaxBytes <= 0 {
		opt.MaxBytes = defaultMaxBytes
	}
	ctx, cancel := context.WithTimeout(ctx, opt.Budget)
	defer cancel()
	w := newWeb(opt.MaxBytes, cancel)
	defer w.close()
	list, builtin := loadLogs(ctx, w)
	if builtin && opt.Note != nil {
		opt.Note("список журналов сертификатов недоступен, взята встроенная копия")
	}
	s := &search{
		web: w, domain: domain, list: list, memo: loadNamesMemo(),
		readers: map[string]*reader{}, found: map[string]bool{},
	}
	tasks := s.prepare(ctx, certs)
	if len(tasks) > 0 && opt.Note != nil {
		opt.Note(fmt.Sprintf("журналы сертификатов: окон %d", len(tasks)))
	}
	s.readAll(ctx, tasks)
	s.memo.save()
	return s.result(ctx, certs)
}

// groupCerts собирает метки по сертификатам: сертификат узнаётся по сроку и набору имён.
func groupCerts(anchors []Anchor) []*certMarks {
	byKey := map[string]*certMarks{}
	var out []*certMarks
	for _, a := range anchors {
		if a.Time.IsZero() {
			continue
		}
		names := map[string]bool{}
		for _, name := range a.Names {
			if name = hostName(name); name != "" {
				names[name] = true
			}
		}
		key := a.NotAfter.UTC().Format(time.RFC3339) + " " + strings.Join(slices.Sorted(maps.Keys(names)), " ")
		c := byKey[key]
		if c == nil {
			c = &certMarks{at: a.Time.UnixMilli(), notAfter: a.NotAfter, names: names}
			byKey[key] = c
			out = append(out, c)
		}
		c.at = min(c.at, a.Time.UnixMilli())
		c.marks = append(c.marks, a)
	}
	return out
}

// merge: свежие окна первыми; окна одного журнала ближе mergeMs склеены, место по номеру важнее места по времени.
func merge(tasks []task) []task {
	fresh := func(a, b task) int {
		return cmp.Or(cmp.Compare(b.at, a.at), cmp.Compare(a.r.log.ID, b.r.log.ID), cmp.Compare(b.index, a.index))
	}
	slices.SortFunc(tasks, func(a, b task) int {
		return cmp.Or(cmp.Compare(a.r.log.ID, b.r.log.ID), fresh(a, b))
	})
	var out []task
	for _, t := range tasks {
		k := len(out) - 1
		if k < 0 || out[k].r != t.r || out[k].at-t.at >= mergeMs {
			out = append(out, t)
			continue
		}
		self := maps.Clone(out[k].self)
		maps.Copy(self, t.self)
		if out[k].index < 0 && t.index >= 0 {
			out[k] = t
		}
		out[k].self = self
	}
	slices.SortFunc(out, fresh)
	return out
}

// scan читает окно от места метки наружу и отдаёт имена домена в нём.
func scan(ctx context.Context, t task) ([]string, error) {
	center := min(t.index/tileWidth, t.r.last)
	if t.index < 0 {
		var err error
		if center, err = t.r.locate(ctx, t.at, windowMs/2); err != nil {
			return nil, err
		}
	}
	w := &window{task: t, got: map[int64]tile{}, lo: center, hi: center, from: t.at - windowMs, to: t.at + windowMs}
	err := w.fetch(ctx, max(center-1, 0), min(center+1, t.r.last))
	if _, ok := w.got[center]; !ok {
		return nil, err
	}
	if _, ok := w.got[center-1]; ok {
		w.lo--
	}
	if _, ok := w.got[center+1]; ok {
		w.hi++
	}
	for {
		left, right := w.need()
		if left+right == 0 {
			if !w.grow() {
				break
			}
			continue
		}
		if room := maxTiles - (w.hi - w.lo + 1); room < left+right {
			left, right = left*room/(left+right), right*room/(left+right)
		}
		if left+right <= 0 || !w.extend(ctx, left, right) {
			break
		}
	}
	return w.names(), nil
}
