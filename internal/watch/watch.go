// Package watch: состояние "работает или лежит" по последовательным опросам, без дребезга.
package watch

import "time"

// State: что известно о сайте сейчас.
type State int

const (
	Unknown State = iota
	Up
	Down
)

// DownAfter: сколько неудач подряд считать падением. Одна - это ещё сеть моргнула.
const DownAfter = 2

// Tracker копит опросы и сообщает только о смене состояния.
type Tracker struct {
	State State
	Since time.Time
	fails int
}

// Event: смена состояния. Was - сколько длилось прежнее.
type Event struct {
	From, To State
	Was      time.Duration
	At       time.Time
}

// Observe учитывает опрос и возвращает событие, если состояние сменилось.
func (t *Tracker) Observe(ok bool, at time.Time) (Event, bool) {
	next := t.State
	if ok {
		t.fails = 0
		next = Up
	} else {
		t.fails++
		if t.fails >= DownAfter {
			next = Down
		}
	}
	if next == t.State {
		return Event{}, false
	}
	ev := Event{From: t.State, To: next, At: at}
	if !t.Since.IsZero() {
		ev.Was = at.Sub(t.Since)
	}
	t.State, t.Since = next, at
	return ev, true
}
