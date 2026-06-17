package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

type pingMsg struct{}

// counter: экран, который считает свои пинги и на каждый заказывает следующий.
type counter struct{ got int }

func (c *counter) Crumbs() []string { return nil }
func (c *counter) Status() string   { return "" }
func (c *counter) Keys() []Key      { return nil }
func (c *counter) Command() string  { return "" }
func (c *counter) View(int, int) string {
	return ""
}

func (c *counter) Init() tea.Cmd { return ping }

func (c *counter) Update(msg tea.Msg, _ *App) tea.Cmd {
	if _, ok := msg.(pingMsg); ok {
		c.got++
		return ping
	}
	return nil
}

func ping() tea.Msg { return pingMsg{} }

// Регрессия: два одинаковых экрана в стопке получали пинги друг друга, и тикеры удваивались.
func TestRepliesReachOnlyOwner(t *testing.T) {
	first, second := &counter{}, &counter{}
	app := &App{stack: []frame{{s: first, id: 1}}, lastID: 1}
	firstTick := app.Init()
	secondTick := app.Push(second)

	_, next := app.Update(firstTick())
	if first.got != 1 || second.got != 0 {
		t.Fatalf("ответ первого: первый %d, второй %d", first.got, second.got)
	}
	app.Update(secondTick())
	if first.got != 1 || second.got != 1 {
		t.Fatalf("ответ второго: первый %d, второй %d", first.got, second.got)
	}

	app.Pop()
	app.Update(next())
	app.Update(secondTick())
	if first.got != 2 || second.got != 1 {
		t.Errorf("после закрытия второго: первый %d, второй %d", first.got, second.got)
	}
}
