package tui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
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

// Одна буква - одно намерение: каждое объявление клавиши в экранах сверяется с keyVerbs.
// Экраны собираются из данных разделов, поэтому сверяется исходник, а не готовые экраны.
func TestKeysFollowVerbs(t *testing.T) {
	fset := token.NewFileSet()
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			if arr, ok := lit.Type.(*ast.ArrayType); ok && typeName(arr.Elt) == "Key" {
				for _, e := range lit.Elts {
					if el, ok := e.(*ast.CompositeLit); ok {
						checkKey(t, fset, el, positional(el))
					}
				}
				return true
			}
			if typeName(lit.Type) == "Key" {
				checkKey(t, fset, lit, positional(lit))
			}
			checkKey(t, fset, lit, named(lit))
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func typeName(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return v.Sel.Name
	}
	return ""
}

func str(e ast.Expr) (string, bool) {
	b, ok := e.(*ast.BasicLit)
	if !ok || b.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(b.Value)
	return s, err == nil
}

// positional: Key{"x", "убрать"}.
func positional(lit *ast.CompositeLit) []string {
	if len(lit.Elts) != 2 {
		return nil
	}
	k, ok1 := str(lit.Elts[0])
	d, ok2 := str(lit.Elts[1])
	if !ok1 || !ok2 {
		return nil
	}
	return []string{k, d}
}

// named: действия экранов - {Key: "x", Label: "убрать", ...}.
func named(lit *ast.CompositeLit) []string {
	var k, d string
	for _, e := range lit.Elts {
		kv, ok := e.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		switch typeName(kv.Key) {
		case "Key":
			k, _ = str(kv.Value)
		case "Label":
			d, _ = str(kv.Value)
		}
	}
	if k == "" || d == "" {
		return nil
	}
	return []string{k, d}
}

func checkKey(t *testing.T, fset *token.FileSet, lit *ast.CompositeLit, kd []string) {
	t.Helper()
	if kd == nil {
		return
	}
	switch {
	case kd[0] == "enter", kd[0] == "esc", kd[0] == "?", kd[0] == ":", kd[0] == "q", kd[0] == "клавиша":
		return
	case strings.Contains(kd[0], " "):
		return // группа клавиш в шпаргалке: "pgup pgdn"
	}
	allowed, ok := keyVerbs[kd[0]]
	if !ok {
		t.Errorf("%s: клавиши %q нет в keyVerbs", fset.Position(lit.Pos()), kd[0])
		return
	}
	if !slices.Contains(allowed, kd[1]) {
		t.Errorf("%s: %q - %q, а в keyVerbs %v", fset.Position(lit.Pos()), kd[0], kd[1], allowed)
	}
}
