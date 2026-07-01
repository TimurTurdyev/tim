// Package watch: состояние "работает или лежит" по последовательным опросам, без дребезга.
package watch

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

// DownAfter: сколько неудач подряд считать падением. Одна - это ещё сеть моргнула.
const DownAfter = 2

// State: что известно о сайте сейчас.
type State int

const (
	Unknown State = iota
	Up
	Down
)

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

// Probe: работает - ответил кодом ниже 500 за отведённое клиенту время; detail - код и время или ошибка.
func Probe(client *http.Client, url string) (ok bool, detail string, err error) {
	start := time.Now()
	resp, err := client.Get(url)
	took := time.Since(start).Round(time.Millisecond)
	if err != nil {
		return false, "", err
	}
	// Дочитать тело, чтобы соединение вернулось в пул: иначе каждый опрос - новый TLS.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	_ = resp.Body.Close()
	return resp.StatusCode < 500, fmt.Sprintf("%d за %v", resp.StatusCode, took), nil
}
