// Package archtest: стражи архитектуры - размер и раскладка файлов, уровни пакетов, выход в систему.
package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const module = "github.com/TimurTurdyev/tim/internal/"

// Ядро ничего не знает о предметной области; разделы - то, что видит человек; остальное - библиотеки.
// app - связка корня и разделов: окружение, выбор проекта, запуск экранов; ей можно в библиотеки.
var (
	core = []string{"brand", "cache", "config", "fuzzy", "gitx", "level", "par", "paths", "privileged",
		"runner", "schedule", "shellq", "tui", "ui"}
	sections = []string{"bypass", "disk", "home", "local", "machine", "motd", "projects", "serve",
		"servers", "shell", "sites", "tools"}
	// Разрешённые зависимости раздела от раздела: строго в одну сторону.
	sectionDeps = map[string][]string{
		"home":    sections,
		"motd":    {"bypass", "machine", "projects", "serve"},
		"bypass":  {"machine"}, // приложение мимо VPN: открытый экземпляр сначала закрывается
		"servers": {"bypass"},  // ssh и туннели заводят маршрут мимо VPN до сервера
		"sites":   {"machine"}, // обход заодно говорит о забытых dev-серверах и прожорливых процессах
	}
	execAllowed = []string{"runner", "privileged"}
)

type goFile struct {
	path string // относительно корня модуля
	pkg  string // каталог под internal/ или "cmd"
	ast  *ast.File
	fset *token.FileSet
	size int
}

func TestFileSize(t *testing.T) {
	for _, f := range sources(t) {
		limit := 500
		if f.pkg == "cmd" {
			limit = 600
		}
		if f.size > limit {
			t.Errorf("%s: %d строк, больше %d - раздели по темам", f.path, f.size, limit)
		}
	}
}

// Константы и переменные пакета - выше типов и функций: читатель видит состояние до кода.
// Исключения: значения перечисления сразу под своим типом и проверки интерфейса var _ = ...
func TestLayout(t *testing.T) {
	for _, f := range sources(t) {
		seenCode := false
		for i, d := range f.ast.Decls {
			g, ok := d.(*ast.GenDecl)
			switch {
			case !ok || g.Tok == token.TYPE:
				seenCode = true
			case g.Tok == token.CONST && enumOf(f.ast.Decls, i):
			case (g.Tok == token.CONST || g.Tok == token.VAR) && seenCode && !blankAssert(g):
				t.Errorf("%s:%d: %s ниже типов и функций - подними в начало файла",
					f.path, f.fset.Position(g.Pos()).Line, g.Tok)
			}
		}
	}
}

func TestLevels(t *testing.T) {
	for _, f := range sources(t) {
		for _, imp := range imports(f) {
			if msg := forbidden(f, imp); msg != "" {
				t.Errorf("%s: импорт %s - %s", f.path, imp, msg)
			}
		}
	}
}

func forbidden(f goFile, imp string) string {
	name := filepath.Base(f.path)
	switch {
	case imp == "os/exec" && !slices.Contains(execAllowed, f.pkg):
		return "внешние команды только через runner"
	case imp == "github.com/spf13/cobra" && f.pkg != "cmd" && !strings.HasPrefix(name, "command"):
		return "cobra только в cmd и command*.go разделов"
	}
	dep, ok := strings.CutPrefix(imp, module)
	if !ok || f.pkg == "cmd" {
		return ""
	}
	dep, _, _ = strings.Cut(dep, "/")
	switch {
	case dep == "tui" && f.pkg != "tui" && level(f.pkg) != "app" && level(f.pkg) != "section":
		return "экраны только в разделах"
	case dep == "tui" && f.pkg != "home" && level(f.pkg) == "section" && !strings.HasPrefix(name, "screen"):
		return "экран раздела живёт в screen*.go"
	case level(f.pkg) == "core" && level(dep) != "core":
		return "ядро не знает о библиотеках и разделах"
	case level(f.pkg) == "lib" && (level(dep) == "section" || level(dep) == "app"):
		return "библиотека не зависит от разделов"
	case level(f.pkg) == "app" && level(dep) == "section":
		return "связка не зависит от разделов"
	case level(f.pkg) == "section" && level(dep) == "section" && dep != f.pkg && !slices.Contains(sectionDeps[f.pkg], dep):
		return "раздел не зависит от другого раздела"
	}
	return ""
}

func level(pkg string) string {
	switch {
	case pkg == "app":
		return "app"
	case slices.Contains(core, pkg):
		return "core"
	case slices.Contains(sections, pkg):
		return "section"
	}
	return "lib"
}

// Константы сразу под типом - его значения (type Kind int; const (KindNet Kind = iota ...)).
func enumOf(decls []ast.Decl, i int) bool {
	if i == 0 {
		return false
	}
	prev, ok := decls[i-1].(*ast.GenDecl)
	return ok && prev.Tok == token.TYPE
}

// var _ Screen = (*x)(nil) - проверка интерфейса, её место рядом с типом.
func blankAssert(g *ast.GenDecl) bool {
	for _, s := range g.Specs {
		v, ok := s.(*ast.ValueSpec)
		if !ok || len(v.Names) != 1 || v.Names[0].Name != "_" {
			return false
		}
	}
	return true
}

func imports(f goFile) []string {
	var list []string
	for _, imp := range f.ast.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		list = append(list, p)
	}
	return list
}

func sources(t *testing.T) []goFile {
	t.Helper()
	root := filepath.Join("..", "..")
	var files []goFile
	for _, dir := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, path, data, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			pkg := "cmd"
			if rest, ok := strings.CutPrefix(filepath.ToSlash(rel), "internal/"); ok {
				pkg, _, _ = strings.Cut(rest, "/")
			}
			files = append(files, goFile{path: rel, pkg: pkg, ast: file, fset: fset, size: strings.Count(string(data), "\n")})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return files
}
