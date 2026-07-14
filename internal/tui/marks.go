package tui

// marks: отмеченное в порядке отметки. Порядок нужен показу: из map строки итога и окна
// подтверждения перемешивались бы при каждой перерисовке.
type marks[T any] struct {
	order []string
	items map[string]T
}

func (m *marks[T]) has(key string) bool {
	_, ok := m.items[key]
	return ok
}

// toggle отмечает или снимает отметку.
func (m *marks[T]) toggle(key string, v T) {
	if m.has(key) {
		delete(m.items, key)
		for i, k := range m.order {
			if k == key {
				m.order = append(m.order[:i], m.order[i+1:]...)
				break
			}
		}
		return
	}
	if m.items == nil {
		m.items = map[string]T{}
	}
	m.items[key] = v
	m.order = append(m.order, key)
}

func (m *marks[T]) list() []T {
	out := make([]T, 0, len(m.order))
	for _, k := range m.order {
		out = append(out, m.items[k])
	}
	return out
}

func (m *marks[T]) keys() []string { return m.order }

func (m *marks[T]) len() int { return len(m.order) }

func (m *marks[T]) clear() { m.order, m.items = nil, nil }
