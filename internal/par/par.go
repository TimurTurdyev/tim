// Package par: параллельный обход с ограничением числа одновременных задач.
package par

import "sync"

// Limit: сколько задач сразу, если особой причины выбирать другое число нет.
const Limit = 8

// Group: задачи, которые добавляются по ходу; одновременно работает не больше limit.
// Go ждёт свободного места, поэтому горутин тоже не больше limit.
type Group struct {
	sem chan struct{}
	wg  sync.WaitGroup
}

// NewGroup: limit < 1 значит Limit.
func NewGroup(limit int) *Group {
	if limit < 1 {
		limit = Limit
	}
	return &Group{sem: make(chan struct{}, limit)}
}

// Go запускает задачу, когда освободится место.
func (g *Group) Go(fn func()) {
	g.sem <- struct{}{}
	g.wg.Go(func() {
		defer func() { <-g.sem }()
		fn()
	})
}

// Wait ждёт все запущенные задачи.
func (g *Group) Wait() { g.wg.Wait() }

// Map применяет fn к каждому элементу, не больше limit сразу; результаты в порядке items.
func Map[T, R any](items []T, limit int, fn func(T) R) []R {
	out := make([]R, len(items))
	g := NewGroup(limit)
	for i, it := range items {
		g.Go(func() { out[i] = fn(it) })
	}
	g.Wait()
	return out
}

// All выполняет функции разом и ждёт все: для нескольких независимых замеров, каждый пишет своё поле.
func All(fns ...func()) {
	var wg sync.WaitGroup
	for _, fn := range fns {
		wg.Go(fn)
	}
	wg.Wait()
}

// Memo: значение по ключу считается один раз; кто спросил тот же ключ параллельно, ждёт первого.
type Memo[K comparable, V any] struct {
	mu   sync.Mutex
	vals map[K]func() V
}

// Get отдаёт значение ключа, считая его fn при первом обращении.
func (m *Memo[K, V]) Get(key K, fn func() V) V {
	m.mu.Lock()
	if m.vals == nil {
		m.vals = map[K]func() V{}
	}
	once, ok := m.vals[key]
	if !ok {
		once = sync.OnceValue(fn)
		m.vals[key] = once
	}
	m.mu.Unlock()
	return once()
}
