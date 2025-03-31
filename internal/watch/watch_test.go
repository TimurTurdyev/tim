package watch

import (
	"testing"
	"time"
)

func TestTransitions(t *testing.T) {
	t0 := time.Date(2025, 10, 20, 10, 0, 0, 0, time.UTC)
	at := func(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }
	var tr Tracker
	var events []Event
	for i, ok := range []bool{true, true, false, true, false, false, false, true} {
		if ev, changed := tr.Observe(ok, at(i*10)); changed {
			events = append(events, ev)
		}
	}
	// Первый успех - "работает"; одиночная неудача на 20 с - не падение; падение на 50 с,
	// подъём на 70 с после 20 с простоя.
	if len(events) != 3 {
		t.Fatalf("%+v", events)
	}
	if events[0].To != Up || events[1].To != Down || !events[1].At.Equal(at(50)) {
		t.Fatalf("%+v", events)
	}
	if events[2].To != Up || events[2].Was != 20*time.Second {
		t.Fatalf("простой %v", events[2].Was)
	}
}

func TestStartsDown(t *testing.T) {
	var tr Tracker
	t0 := time.Now()
	if _, changed := tr.Observe(false, t0); changed {
		t.Fatal("одна неудача на старте - ещё не падение")
	}
	if ev, changed := tr.Observe(false, t0.Add(time.Second)); !changed || ev.To != Down || ev.From != Unknown {
		t.Fatalf("%+v %v", ev, changed)
	}
}
