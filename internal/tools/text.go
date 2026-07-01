package tools

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"rsc.io/qr"

	"github.com/TimurTurdyev/tim/internal/conv"
	"github.com/TimurTurdyev/tim/internal/cronx"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Полублоки: верхняя половина, нижняя, обе. Записаны кодами, чтобы исходник был набран с клавиатуры.
const (
	blockUpper = "\u2580"
	blockLower = "\u2584"
	blockFull  = "\u2588"
)

type passOptions struct {
	length, count          int
	noSymbols, noAmbiguous bool
}

func printQR(args []string, invert bool) error {
	text, err := textOrStdin(args)
	if err != nil {
		return err
	}
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return ui.Errorf("QR: %v", err)
	}
	// Рамка из светлых модулей нужна сканеру. На тёмном фоне светлые модули - блоки, тёмные - пробелы; --invert наоборот.
	const quiet = 2
	size := code.Size + 2*quiet
	black := func(x, y int) bool {
		x, y = x-quiet, y-quiet
		return x >= 0 && y >= 0 && x < code.Size && y < code.Size && code.Black(x, y)
	}
	var b strings.Builder
	for y := 0; y < size; y += 2 {
		for x := range size {
			top, bottom := black(x, y) == invert, black(x, y+1) == invert
			switch {
			case top && bottom:
				b.WriteString(blockFull)
			case top:
				b.WriteString(blockUpper)
			case bottom:
				b.WriteString(blockLower)
			default:
				b.WriteString(" ")
			}
		}
		b.WriteString("\n")
	}
	fmt.Print(b.String())
	return nil
}

func urlEncode(args []string, path bool) error {
	s, err := textOrStdin(args)
	if err != nil {
		return err
	}
	if path {
		fmt.Println(url.PathEscape(s))
	} else {
		fmt.Println(url.QueryEscape(s))
	}
	return nil
}

func urlDecode(args []string) error {
	s, err := textOrStdin(args)
	if err != nil {
		return err
	}
	out, err := url.QueryUnescape(s)
	if err != nil {
		if out, err = url.PathUnescape(s); err != nil {
			return ui.Errorf("не раскодировать: %v", err)
		}
	}
	fmt.Println(out)
	return nil
}

func urlParse(args []string) error {
	s, err := textOrStdin(args)
	if err != nil {
		return err
	}
	u, err := url.Parse(s)
	if err != nil {
		return err
	}
	row := func(k, v string) {
		if v != "" {
			ui.Println(ui.Label(ui.Pad(k, 9)) + " " + v)
		}
	}
	row("схема", u.Scheme)
	if u.User != nil {
		row("логин", u.User.Username())
	}
	row("хост", u.Hostname())
	row("порт", u.Port())
	row("путь", u.Path)
	q := u.Query()
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		for _, v := range q[k] {
			ui.Println(ui.Label("  "+ui.Pad(k, 18)) + " " + v)
		}
	}
	row("фрагмент", u.Fragment)
	return nil
}

func printPasswords(o passOptions) error {
	for range max(o.count, 1) {
		p, err := conv.Password(o.length, !o.noSymbols, o.noAmbiguous)
		if err != nil {
			return ui.Errorf("%v", err)
		}
		fmt.Println(p)
	}
	return nil
}

func printCron(args []string, runs int) error {
	expr := strings.Join(args, " ")
	s, err := cronx.Parse(expr)
	if err != nil {
		return ui.Errorf("%s: %v", expr, err)
	}
	ui.Println(ui.Label(s.Describe()))
	for _, t := range s.Next(time.Now(), runs) {
		ui.Println("  " + t.Format("Mon 2006-01-02 15:04") + ui.Dim("  "+relative(t)))
	}
	return nil
}
